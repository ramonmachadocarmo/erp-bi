package application

import (
	"context"
	"sort"
	"strings"
	"time"

	"erp/pkg/codes"
	"erp/services/bi-service/internal/domain"
)

type Service struct {
	forecasts      domain.ForecastRepository
	budgets        domain.BudgetRepository
	schedules      domain.ScheduleRepository
	supplierPrices domain.SupplierPriceRepository
	sales          domain.SalesHistory
	catalog        domain.Catalog
	purchasing     domain.Purchasing
	budgetSeq      codes.Sequence
}

func New(
	forecasts domain.ForecastRepository,
	budgets domain.BudgetRepository,
	schedules domain.ScheduleRepository,
	supplierPrices domain.SupplierPriceRepository,
	sales domain.SalesHistory,
	catalog domain.Catalog,
	purchasing domain.Purchasing,
	budgetSeq codes.Sequence,
) *Service {
	return &Service{
		forecasts:      forecasts,
		budgets:        budgets,
		schedules:      schedules,
		supplierPrices: supplierPrices,
		sales:          sales,
		catalog:        catalog,
		purchasing:     purchasing,
		budgetSeq:      budgetSeq,
	}
}

type forecastCalc struct {
	computed  float64
	override  *float64
	effective float64
}

// loadOverrides splits the stored manual overrides by kind: PRODUCT overrides
// sit alongside a computed moving average, KIT overrides have no computed
// signal of their own and exist purely to be expanded into component demand.
func (s *Service) loadOverrides(ctx context.Context) (productOverrides, kitOverrides map[string]float64, err error) {
	overrides, err := s.forecasts.ListOverrides(ctx)
	if err != nil {
		return nil, nil, err
	}
	productOverrides = map[string]float64{}
	kitOverrides = map[string]float64{}
	for _, o := range overrides {
		if o.Kind == domain.ForecastKindKit {
			kitOverrides[o.TargetID] = o.WeeklyQty
		} else {
			productOverrides[o.TargetID] = o.WeeklyQty
		}
	}
	return productOverrides, kitOverrides, nil
}

// productForecastCalc scopes to any product that has sales history in the
// lookback window OR a manual override set, so newly-added products with no
// history yet are still forecastable via an override, and once real sales
// history accumulates it naturally joins the scope on its own.
func (s *Service) productForecastCalc(ctx context.Context, lookbackWeeks int) (map[string]forecastCalc, error) {
	if lookbackWeeks <= 0 {
		return nil, domain.ErrInvalid
	}
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -7*lookbackWeeks)
	salesQty, err := s.sales.OrderQuantities(ctx, from, to)
	if err != nil {
		return nil, err
	}
	productOverrides, _, err := s.loadOverrides(ctx)
	if err != nil {
		return nil, err
	}
	ids := map[string]struct{}{}
	for id := range salesQty {
		ids[id] = struct{}{}
	}
	for id := range productOverrides {
		ids[id] = struct{}{}
	}
	out := make(map[string]forecastCalc, len(ids))
	for id := range ids {
		computed := domain.MovingAverageWeekly(salesQty[id], lookbackWeeks)
		var overridePtr *float64
		if v, ok := productOverrides[id]; ok {
			vv := v
			overridePtr = &vv
		}
		out[id] = forecastCalc{
			computed:  computed,
			override:  overridePtr,
			effective: domain.EffectiveWeeklyQty(computed, overridePtr),
		}
	}
	return out, nil
}

// combinedProductDemand is the shared calc core for StoragePlan/GenerateBudget:
// every genuinely-purchasable product's own direct demand (computed or
// overridden), plus every kit's effective weekly qty (see effectiveKitQty)
// expanded through its recipe onto each component's demand. A kit's linked
// final product is never added as itself — see kitByProductID — since it
// isn't something you buy from a supplier, only something assembled from the
// products this loop adds it as instead.
func (s *Service) combinedProductDemand(ctx context.Context, lookbackWeeks int) (map[string]float64, error) {
	calc, err := s.productForecastCalc(ctx, lookbackWeeks)
	if err != nil {
		return nil, err
	}
	_, kitOverrides, err := s.loadOverrides(ctx)
	if err != nil {
		return nil, err
	}
	assemblies, err := s.assemblyLookup(ctx)
	if err != nil {
		return nil, err
	}
	kitProducts := kitByProductID(assemblies)

	demand := make(map[string]float64, len(calc))
	for id, c := range calc {
		if _, isKit := kitProducts[id]; isKit {
			continue
		}
		demand[id] = c.effective
	}
	for _, a := range assemblies {
		_, _, effective := effectiveKitQty(a, calc, kitOverrides)
		if effective <= 0 {
			continue
		}
		for _, item := range a.Items {
			demand[item.ProductID] += effective * item.Quantity
		}
	}
	return demand, nil
}

// kitByProductID indexes assemblies by their linked final product — a kit
// sells as this product, so its real sales land in productForecastCalc under
// this ID. Every place that computes purchasable demand or lists per-product
// forecasts must exclude these IDs and route their quantity through
// effectiveKitQty/component expansion instead: the finished kit is never
// itself purchased from a supplier, only assembled from its components.
func kitByProductID(assemblies map[string]domain.Assembly) map[string]domain.Assembly {
	m := make(map[string]domain.Assembly, len(assemblies))
	for _, a := range assemblies {
		if a.ProductID != "" {
			m[a.ProductID] = a
		}
	}
	return m
}

// effectiveKitQty is a kit's weekly quantity: a manual override always wins
// when set, exactly like a product-level override wins over its computed
// value; otherwise it falls back to the linked product's own computed moving
// average, since a kit sells as that product and therefore has real sales
// history of its own once it starts selling. A brand-new kit with neither an
// override nor any sales yet evaluates to zero.
//
// The override can come from either of two places: a proper KIT-kind entry
// (kitOverrides, keyed by assembly ID — the intended path from the "Kit"
// forecast input) or a PRODUCT-kind override that was set directly on the
// kit's own linked product ID. The latter shouldn't happen going forward, but
// existing data has it — before this function existed, nothing filtered a
// kit's linked product out of the plain product-forecast input, so some kits
// (see the "Cesta N pessoas" family) have their manual weekly qty stored as a
// PRODUCT override on their own product ID instead. Treating both the same
// way means those existing overrides keep working (and keep expanding into
// components) instead of silently going to zero the moment this shipped.
func effectiveKitQty(a domain.Assembly, calc map[string]forecastCalc, kitOverrides map[string]float64) (computed float64, override *float64, effective float64) {
	var productCalc forecastCalc
	if a.ProductID != "" {
		productCalc = calc[a.ProductID]
	}
	computed = productCalc.computed
	if v, ok := kitOverrides[a.ID]; ok {
		vv := v
		override = &vv
	} else {
		override = productCalc.override
	}
	effective = domain.EffectiveWeeklyQty(computed, override)
	return computed, override, effective
}

func (s *Service) productLookup(ctx context.Context) (map[string]domain.Product, error) {
	products, err := s.catalog.Products(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]domain.Product, len(products))
	for _, p := range products {
		m[p.ID] = p
	}
	return m, nil
}

func (s *Service) assemblyLookup(ctx context.Context) (map[string]domain.Assembly, error) {
	assemblies, err := s.catalog.Assemblies(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]domain.Assembly, len(assemblies))
	for _, a := range assemblies {
		m[a.ID] = a
	}
	return m, nil
}

// ListForecasts returns one entry per genuinely-purchasable product with
// sales history or a manual override, plus one entry per kit that has either
// a manual weekly forecast or real sales history of its own (kits sell as
// their linked final product — see effectiveKitQty). A kit's linked product
// is never also listed under Kind PRODUCT (see kitByProductID) — it would
// double the same demand under two rows and, for Financials, double its
// revenue. Excluded entries are dropped unless includeExcluded is set (used
// to list them for restoring).
func (s *Service) ListForecasts(ctx context.Context, lookbackWeeks int, includeExcluded bool) ([]domain.ForecastEntry, error) {
	calc, err := s.productForecastCalc(ctx, lookbackWeeks)
	if err != nil {
		return nil, err
	}
	_, kitOverrides, err := s.loadOverrides(ctx)
	if err != nil {
		return nil, err
	}
	products, err := s.productLookup(ctx)
	if err != nil {
		return nil, err
	}
	assemblies, err := s.assemblyLookup(ctx)
	if err != nil {
		return nil, err
	}
	kitProducts := kitByProductID(assemblies)
	exclusions, err := s.forecasts.ListExclusions(ctx)
	if err != nil {
		return nil, err
	}
	excluded := make(map[[2]string]bool, len(exclusions))
	for _, e := range exclusions {
		excluded[[2]string{e.Kind, e.TargetID}] = true
	}

	out := make([]domain.ForecastEntry, 0, len(calc)+len(assemblies))
	for id, c := range calc {
		if _, isKit := kitProducts[id]; isKit {
			continue
		}
		if excluded[[2]string{domain.ForecastKindProduct, id}] && !includeExcluded {
			continue
		}
		p := products[id]
		out = append(out, domain.ForecastEntry{
			Kind:               domain.ForecastKindProduct,
			TargetID:           id,
			Code:               p.SKU,
			Name:               p.Name,
			ComputedWeeklyQty:  c.computed,
			OverrideWeeklyQty:  c.override,
			EffectiveWeeklyQty: c.effective,
			LookbackWeeks:      lookbackWeeks,
			Excluded:           excluded[[2]string{domain.ForecastKindProduct, id}],
		})
	}
	for _, a := range assemblies {
		computed, override, effective := effectiveKitQty(a, calc, kitOverrides)
		if computed <= 0 && override == nil {
			continue
		}
		if excluded[[2]string{domain.ForecastKindKit, a.ID}] && !includeExcluded {
			continue
		}
		out = append(out, domain.ForecastEntry{
			Kind:               domain.ForecastKindKit,
			TargetID:           a.ID,
			Code:               a.Code,
			Name:               a.Name,
			ComputedWeeklyQty:  computed,
			OverrideWeeklyQty:  override,
			EffectiveWeeklyQty: effective,
			LookbackWeeks:      lookbackWeeks,
			Excluded:           excluded[[2]string{domain.ForecastKindKit, a.ID}],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Code < out[j].Code
	})
	return out, nil
}

// Financials projects each forecasted product/kit's weekly revenue, cost,
// and profit ("Receita x Despesa"). A kit's revenue comes from its own
// linked product's sale price; its cost is the assembly's full cost (every
// component/support item's purchase price × quantity per kit — set up on
// the Montagem screen, so packaging like a bag counts once it's added as an
// item there). A directly-sold product uses its own sale/purchase price.
// Items with neither price registered are skipped rather than shown as a
// misleading zero.
func (s *Service) Financials(ctx context.Context, lookbackWeeks int) ([]domain.FinancialLine, error) {
	forecasts, err := s.ListForecasts(ctx, lookbackWeeks, false)
	if err != nil {
		return nil, err
	}
	products, err := s.productLookup(ctx)
	if err != nil {
		return nil, err
	}
	assemblies, err := s.assemblyLookup(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]domain.FinancialLine, 0, len(forecasts))
	for _, f := range forecasts {
		var unitRevenue, unitCost float64
		if f.Kind == domain.ForecastKindKit {
			a, ok := assemblies[f.TargetID]
			if !ok {
				continue
			}
			unitCost = a.Cost
			if p, ok := products[a.ProductID]; ok {
				unitRevenue = p.SalePrice
			}
		} else {
			p, ok := products[f.TargetID]
			if !ok {
				continue
			}
			unitRevenue = p.SalePrice
			unitCost = p.PurchasePrice
		}
		if unitRevenue <= 0 && unitCost <= 0 {
			continue
		}
		out = append(out, domain.FinancialLineFor(f, unitRevenue, unitCost))
	}
	return out, nil
}

func (s *Service) ExcludeFromForecast(ctx context.Context, kind, targetID string) error {
	if kind != domain.ForecastKindProduct && kind != domain.ForecastKindKit {
		return domain.ErrInvalid
	}
	return s.forecasts.SetExclusion(ctx, kind, targetID)
}

func (s *Service) IncludeInForecast(ctx context.Context, kind, targetID string) error {
	return s.forecasts.DeleteExclusion(ctx, kind, targetID)
}

func (s *Service) SetForecastOverride(ctx context.Context, kind, targetID string, weeklyQty float64, note, updatedBy string) (domain.ForecastOverride, error) {
	if kind != domain.ForecastKindProduct && kind != domain.ForecastKindKit {
		return domain.ForecastOverride{}, domain.ErrInvalid
	}
	if targetID == "" || weeklyQty < 0 {
		return domain.ForecastOverride{}, domain.ErrInvalid
	}
	return s.forecasts.SetOverride(ctx, domain.ForecastOverride{
		Kind:      kind,
		TargetID:  targetID,
		WeeklyQty: weeklyQty,
		Note:      note,
		UpdatedBy: updatedBy,
	})
}

func (s *Service) ClearForecastOverride(ctx context.Context, kind, targetID string) error {
	return s.forecasts.DeleteOverride(ctx, kind, targetID)
}

func (s *Service) StoragePlan(ctx context.Context, coverageWeeks int, safetyPercent float64, lookbackWeeks int) ([]domain.StoragePlanLine, error) {
	if coverageWeeks <= 0 || safetyPercent < 0 {
		return nil, domain.ErrInvalid
	}
	demand, err := s.combinedProductDemand(ctx, lookbackWeeks)
	if err != nil {
		return nil, err
	}
	products, err := s.productLookup(ctx)
	if err != nil {
		return nil, err
	}
	balances, err := s.catalog.Balances(ctx)
	if err != nil {
		return nil, err
	}
	onHand := map[string]float64{}
	for _, b := range balances {
		onHand[b.ProductID] += b.QuantityAvailable
	}
	openPO, err := s.purchasing.OpenOrderQuantities(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.StoragePlanLine, 0, len(demand))
	for id, qty := range demand {
		p := products[id]
		on := onHand[id]
		open := openPO[id]
		out = append(out, domain.StoragePlanLine{
			ProductID:   id,
			SKU:         p.SKU,
			Name:        p.Name,
			ForecastQty: qty,
			OnHandQty:   on,
			OpenPOQty:   open,
			NeededQty:   domain.NeededQty(qty, coverageWeeks, safetyPercent, on, open),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProductID < out[j].ProductID })
	return out, nil
}

// GenerateBudget snapshots the current storage plan into a new DRAFT budget,
// keeping only products that actually need restocking. Called both by the
// ad-hoc POST /budgets endpoint (scheduleID nil) and by RunSchedule.
// bestSupplierUnitPrice picks, among the registered supplier prices for a
// product, the one that's actually cheapest per stock/sale unit — not the
// one with the lowest sticker price. A supplier price is "$Price for MinQty
// units of the product's purchase UoM" (e.g. "$12 for 1.5 CX"), so the rule
// of three is: price per purchase-unit (Price/MinQty), then converted into
// price per stock-unit via the product's registered UoM conversion (e.g. 1
// CX = 20 KG → $/CX ÷ 20 = $/KG). A price whose purchase UoM can't be
// converted to the stock UoM is skipped rather than guessed at.
func bestSupplierUnitPrice(product domain.Product, prices []domain.SupplierPrice) (domain.SupplierPrice, float64, bool) {
	stockUoM := product.StockUnitOfMeasure()
	var best domain.SupplierPrice
	bestUnitPrice := 0.0
	found := false
	for _, sp := range prices {
		if sp.ProductID != product.ID || sp.MinQty <= 0 {
			continue
		}
		perPurchaseUnit := sp.Price / sp.MinQty
		stockUnitsPerPurchaseUnit, ok := domain.ConvertQty(1, product.PurchaseUoM, stockUoM, product.Conversions)
		if !ok || stockUnitsPerPurchaseUnit <= 0 {
			continue
		}
		unitPrice := perPurchaseUnit / stockUnitsPerPurchaseUnit
		if !found || unitPrice < bestUnitPrice {
			best = sp
			bestUnitPrice = unitPrice
			found = true
		}
	}
	return best, bestUnitPrice, found
}

// GenerateBudget snapshots the current storage plan into a new budget,
// keeping only products that actually need restocking, then auto-allocates
// each item to its cheapest-per-unit registered supplier (see
// bestSupplierUnitPrice) — best-effort, an item with no registered supplier
// price simply stays unallocated. Real purchasing-service quotes still only
// get created when the budget is explicitly confirmed.
func (s *Service) GenerateBudget(ctx context.Context, coverageWeeks int, safetyPercent float64, lookbackWeeks int, scheduleID *string) (domain.Budget, error) {
	lines, err := s.StoragePlan(ctx, coverageWeeks, safetyPercent, lookbackWeeks)
	if err != nil {
		return domain.Budget{}, err
	}
	code, err := codes.Assign(ctx, "", s.budgetSeq)
	if err != nil {
		return domain.Budget{}, err
	}
	items := make([]domain.BudgetItem, 0, len(lines))
	for _, l := range lines {
		if l.NeededQty <= 0 {
			continue
		}
		items = append(items, domain.BudgetItem{
			ProductID:   l.ProductID,
			ForecastQty: l.ForecastQty,
			OnHandQty:   l.OnHandQty,
			OpenPOQty:   l.OpenPOQty,
			NeededQty:   l.NeededQty,
		})
	}
	b := domain.Budget{
		Code:          code,
		Status:        domain.BudgetStatusDraft,
		CoverageWeeks: coverageWeeks,
		SafetyPercent: safetyPercent,
		LookbackWeeks: lookbackWeeks,
		ScheduleID:    scheduleID,
		Items:         items,
	}
	created, err := s.budgets.Create(ctx, b)
	if err != nil {
		return domain.Budget{}, err
	}

	products, err := s.productLookup(ctx)
	if err == nil {
		if supplierPrices, spErr := s.supplierPrices.List(ctx); spErr == nil {
			for _, item := range created.Items {
				p, ok := products[item.ProductID]
				if !ok {
					continue
				}
				sp, unitPrice, foundPrice := bestSupplierUnitPrice(p, supplierPrices)
				if !foundPrice {
					continue
				}
				_, _ = s.AllocateBudgetItem(ctx, created.ID, item.ID, sp.SupplierID, item.NeededQty, unitPrice)
			}
		}
	}
	return s.budgets.Get(ctx, created.ID)
}

func (s *Service) ListBudgets(ctx context.Context) ([]domain.Budget, error) {
	return s.budgets.List(ctx)
}

func (s *Service) GetBudget(ctx context.Context, id string) (domain.Budget, error) {
	return s.budgets.Get(ctx, id)
}

func (s *Service) UpdateBudgetItem(ctx context.Context, budgetID, itemID string, neededQty float64) (domain.BudgetItem, error) {
	if neededQty < 0 {
		return domain.BudgetItem{}, domain.ErrInvalid
	}
	b, err := s.budgets.Get(ctx, budgetID)
	if err != nil {
		return domain.BudgetItem{}, err
	}
	if b.Status != domain.BudgetStatusDraft && b.Status != domain.BudgetStatusAllocated {
		return domain.BudgetItem{}, domain.ErrInvalid
	}
	found := false
	for _, it := range b.Items {
		if it.ID == itemID {
			found = true
			break
		}
	}
	if !found {
		return domain.BudgetItem{}, domain.ErrNotFound
	}
	return s.budgets.UpdateItemNeededQty(ctx, itemID, neededQty)
}

func (s *Service) DeleteBudgetItem(ctx context.Context, budgetID, itemID string) error {
	b, err := s.budgets.Get(ctx, budgetID)
	if err != nil {
		return err
	}
	if b.Status != domain.BudgetStatusDraft && b.Status != domain.BudgetStatusAllocated {
		return domain.ErrInvalid
	}
	found := false
	for _, it := range b.Items {
		if it.ID != itemID {
			continue
		}
		found = true
		for _, a := range it.Allocations {
			if a.QuoteID != nil {
				return domain.ErrInUse
			}
		}
	}
	if !found {
		return domain.ErrNotFound
	}
	return s.budgets.DeleteItem(ctx, budgetID, itemID)
}

func (s *Service) AllocateBudgetItem(ctx context.Context, budgetID, itemID, supplierID string, quantity, unitPrice float64) (domain.BudgetAllocation, error) {
	if supplierID == "" || quantity <= 0 {
		return domain.BudgetAllocation{}, domain.ErrInvalid
	}
	b, err := s.budgets.Get(ctx, budgetID)
	if err != nil {
		return domain.BudgetAllocation{}, err
	}
	if b.Status != domain.BudgetStatusDraft && b.Status != domain.BudgetStatusAllocated {
		return domain.BudgetAllocation{}, domain.ErrInvalid
	}
	item, err := s.budgets.GetItem(ctx, budgetID, itemID)
	if err != nil {
		return domain.BudgetAllocation{}, err
	}
	already, err := s.budgets.ItemAllocatedQty(ctx, itemID)
	if err != nil {
		return domain.BudgetAllocation{}, err
	}
	if already+quantity > item.NeededQty+1e-9 {
		return domain.BudgetAllocation{}, domain.ErrInvalid
	}
	alloc, err := s.budgets.AddAllocation(ctx, domain.BudgetAllocation{
		BudgetItemID: itemID,
		SupplierID:   supplierID,
		Quantity:     quantity,
		UnitPrice:    unitPrice,
	})
	if err != nil {
		return domain.BudgetAllocation{}, err
	}
	if b.Status == domain.BudgetStatusDraft {
		if err := s.budgets.UpdateStatus(ctx, budgetID, domain.BudgetStatusAllocated); err != nil {
			return domain.BudgetAllocation{}, err
		}
	}
	return alloc, nil
}

func (s *Service) DeleteBudgetAllocation(ctx context.Context, budgetID, allocationID string) error {
	b, err := s.budgets.Get(ctx, budgetID)
	if err != nil {
		return err
	}
	if b.Status != domain.BudgetStatusDraft && b.Status != domain.BudgetStatusAllocated {
		return domain.ErrInvalid
	}
	return s.budgets.DeleteAllocation(ctx, allocationID)
}

// ConfirmBudget groups every not-yet-quoted allocation by supplier and issues
// one purchasing-service quote per supplier. Allocations that already carry a
// quote_id are skipped, so a partially-failed confirm can be safely retried.
// Allocation quantities/prices are tracked in the product's stock UoM
// (needed_qty terms), but a real purchase order has to be placed in the
// product's purchase UoM, in whatever pack size the supplier actually sells
// — domain.PurchaseLine converts and rounds up to a whole pack when a
// registered supplier price is known (see its doc comment).
func (s *Service) ConfirmBudget(ctx context.Context, budgetID string) (domain.Budget, error) {
	b, err := s.budgets.Get(ctx, budgetID)
	if err != nil {
		return domain.Budget{}, err
	}
	if b.Status != domain.BudgetStatusDraft && b.Status != domain.BudgetStatusAllocated {
		return domain.Budget{}, domain.ErrInvalid
	}
	products, err := s.productLookup(ctx)
	if err != nil {
		return domain.Budget{}, err
	}
	supplierPrices, err := s.supplierPrices.List(ctx)
	if err != nil {
		return domain.Budget{}, err
	}
	type priceKey struct{ productID, supplierID string }
	priceLookup := make(map[priceKey]domain.SupplierPrice, len(supplierPrices))
	for _, sp := range supplierPrices {
		priceLookup[priceKey{sp.ProductID, sp.SupplierID}] = sp
	}

	type supplierGroup struct {
		items    []domain.QuoteItem
		allocIDs []string
	}
	groups := map[string]*supplierGroup{}
	for _, item := range b.Items {
		p, ok := products[item.ProductID]
		if !ok {
			continue
		}
		for _, a := range item.Allocations {
			if a.QuoteID != nil {
				continue
			}
			g, ok := groups[a.SupplierID]
			if !ok {
				g = &supplierGroup{}
				groups[a.SupplierID] = g
			}
			var sp *domain.SupplierPrice
			if found, ok := priceLookup[priceKey{item.ProductID, a.SupplierID}]; ok {
				sp = &found
			}
			purchaseQty, unitPrice, convOK := domain.PurchaseLine(a.Quantity, p, sp, a.UnitPrice)
			if !convOK {
				return domain.Budget{}, domain.ErrInvalid
			}
			g.items = append(g.items, domain.QuoteItem{ProductID: item.ProductID, Quantity: purchaseQty, UnitPrice: unitPrice})
			g.allocIDs = append(g.allocIDs, a.ID)
		}
	}
	if len(groups) == 0 {
		return domain.Budget{}, domain.ErrInvalid
	}
	for supplierID, g := range groups {
		quoteID, err := s.purchasing.CreateQuote(ctx, supplierID, g.items, "orcamento "+b.Code)
		if err != nil {
			return domain.Budget{}, err
		}
		for _, allocID := range g.allocIDs {
			if err := s.budgets.SetAllocationQuote(ctx, allocID, quoteID); err != nil {
				return domain.Budget{}, err
			}
		}
	}
	if err := s.budgets.UpdateStatus(ctx, budgetID, domain.BudgetStatusConfirmed); err != nil {
		return domain.Budget{}, err
	}
	return s.budgets.Get(ctx, budgetID)
}

func (s *Service) CancelBudget(ctx context.Context, budgetID string) error {
	b, err := s.budgets.Get(ctx, budgetID)
	if err != nil {
		return err
	}
	if b.Status != domain.BudgetStatusDraft && b.Status != domain.BudgetStatusAllocated {
		return domain.ErrInvalid
	}
	return s.budgets.UpdateStatus(ctx, budgetID, domain.BudgetStatusCancelled)
}

// DeleteBudget permanently removes a budget and its items/allocations. Confirmed
// budgets are refused since they already generated real purchasing-service
// quotes — cancel or leave those in place instead of deleting the record.
func (s *Service) DeleteBudget(ctx context.Context, budgetID string) error {
	b, err := s.budgets.Get(ctx, budgetID)
	if err != nil {
		return err
	}
	if b.Status == domain.BudgetStatusConfirmed {
		return domain.ErrInvalid
	}
	return s.budgets.Delete(ctx, budgetID)
}

func (s *Service) ListSupplierPrices(ctx context.Context) ([]domain.SupplierPrice, error) {
	return s.supplierPrices.List(ctx)
}

func (s *Service) SetSupplierPrice(ctx context.Context, productID, supplierID string, price, minQty float64) (domain.SupplierPrice, error) {
	if productID == "" || supplierID == "" || price < 0 || minQty <= 0 {
		return domain.SupplierPrice{}, domain.ErrInvalid
	}
	return s.supplierPrices.Upsert(ctx, domain.SupplierPrice{ProductID: productID, SupplierID: supplierID, Price: price, MinQty: minQty})
}

func (s *Service) DeleteSupplierPrice(ctx context.Context, id string) error {
	return s.supplierPrices.Delete(ctx, id)
}

func validateSchedule(sch domain.BudgetSchedule) error {
	if strings.TrimSpace(sch.Name) == "" {
		return domain.ErrInvalid
	}
	if sch.Frequency != domain.ScheduleFrequencyWeekly && sch.Frequency != domain.ScheduleFrequencyMonthly {
		return domain.ErrInvalid
	}
	if sch.Frequency == domain.ScheduleFrequencyWeekly {
		if sch.DayOfWeek == nil || *sch.DayOfWeek < 0 || *sch.DayOfWeek > 6 {
			return domain.ErrInvalid
		}
	}
	if sch.Frequency == domain.ScheduleFrequencyMonthly {
		if sch.DayOfMonth == nil || *sch.DayOfMonth < 1 || *sch.DayOfMonth > 31 {
			return domain.ErrInvalid
		}
	}
	if sch.CoverageWeeks <= 0 || sch.LookbackWeeks <= 0 || sch.SafetyPercent < 0 {
		return domain.ErrInvalid
	}
	return nil
}

func (s *Service) CreateSchedule(ctx context.Context, sch domain.BudgetSchedule) (domain.BudgetSchedule, error) {
	if err := validateSchedule(sch); err != nil {
		return domain.BudgetSchedule{}, err
	}
	sch.NextRunAt = domain.FirstRunAt(sch.Frequency, sch.DayOfWeek, sch.DayOfMonth, time.Now())
	return s.schedules.Create(ctx, sch)
}

func (s *Service) GetSchedule(ctx context.Context, id string) (domain.BudgetSchedule, error) {
	return s.schedules.Get(ctx, id)
}

func (s *Service) ListSchedules(ctx context.Context) ([]domain.BudgetSchedule, error) {
	return s.schedules.List(ctx)
}

func (s *Service) UpdateSchedule(ctx context.Context, id string, sch domain.BudgetSchedule) (domain.BudgetSchedule, error) {
	if err := validateSchedule(sch); err != nil {
		return domain.BudgetSchedule{}, err
	}
	sch.ID = id
	sch.NextRunAt = domain.FirstRunAt(sch.Frequency, sch.DayOfWeek, sch.DayOfMonth, time.Now())
	return s.schedules.Update(ctx, sch)
}

func (s *Service) DeleteSchedule(ctx context.Context, id string) error {
	return s.schedules.Delete(ctx, id)
}

// RunSchedule generates a DRAFT budget for the schedule's parameters and
// advances next_run_at. Used by both the ticker loop and POST /schedules/:id/run-now.
func (s *Service) RunSchedule(ctx context.Context, sch domain.BudgetSchedule) (domain.Budget, error) {
	scheduleID := sch.ID
	b, err := s.GenerateBudget(ctx, sch.CoverageWeeks, sch.SafetyPercent, sch.LookbackWeeks, &scheduleID)
	if err != nil {
		return domain.Budget{}, err
	}
	now := time.Now().UTC()
	next := domain.AdvanceRunAt(sch.Frequency, sch.DayOfMonth, sch.NextRunAt)
	if err := s.schedules.MarkRun(ctx, sch.ID, now, next); err != nil {
		return domain.Budget{}, err
	}
	return b, nil
}
