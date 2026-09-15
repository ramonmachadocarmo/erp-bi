package application

import (
	"context"
	"testing"
	"time"

	"erp/services/bi-service/internal/domain"
)

// --- fakes ---

type fakeForecastRepo struct {
	overrides  []domain.ForecastOverride
	exclusions []domain.ForecastExclusion
}

func (f *fakeForecastRepo) ListOverrides(ctx context.Context) ([]domain.ForecastOverride, error) {
	return f.overrides, nil
}

func (f *fakeForecastRepo) SetOverride(ctx context.Context, o domain.ForecastOverride) (domain.ForecastOverride, error) {
	f.overrides = append(f.overrides, o)
	return o, nil
}

func (f *fakeForecastRepo) DeleteOverride(ctx context.Context, kind, targetID string) error {
	return nil
}

func (f *fakeForecastRepo) ListExclusions(ctx context.Context) ([]domain.ForecastExclusion, error) {
	return f.exclusions, nil
}

func (f *fakeForecastRepo) SetExclusion(ctx context.Context, kind, targetID string) error {
	return nil
}

func (f *fakeForecastRepo) DeleteExclusion(ctx context.Context, kind, targetID string) error {
	return nil
}

type fakeCatalog struct {
	products   []domain.Product
	balances   []domain.Balance
	assemblies []domain.Assembly
}

func (c *fakeCatalog) Products(ctx context.Context) ([]domain.Product, error) { return c.products, nil }
func (c *fakeCatalog) Balances(ctx context.Context) ([]domain.Balance, error) { return c.balances, nil }
func (c *fakeCatalog) Assemblies(ctx context.Context) ([]domain.Assembly, error) {
	return c.assemblies, nil
}

type fakeSalesHistory struct {
	qty map[string]float64
}

func (s *fakeSalesHistory) OrderQuantities(ctx context.Context, from, to time.Time) (map[string]float64, error) {
	return s.qty, nil
}

type fakePurchasing struct {
	open map[string]float64
}

func (p *fakePurchasing) OpenOrderQuantities(ctx context.Context) (map[string]float64, error) {
	return p.open, nil
}

func (p *fakePurchasing) CreateQuote(ctx context.Context, supplierID string, items []domain.QuoteItem, notes string) (string, error) {
	return "", nil
}

type fakeSupplierPrices struct{}

func (fakeSupplierPrices) List(ctx context.Context) ([]domain.SupplierPrice, error) { return nil, nil }
func (fakeSupplierPrices) Upsert(ctx context.Context, sp domain.SupplierPrice) (domain.SupplierPrice, error) {
	return sp, nil
}
func (fakeSupplierPrices) Delete(ctx context.Context, id string) error { return nil }

type fakeBudgets struct{}

func (fakeBudgets) Create(ctx context.Context, b domain.Budget) (domain.Budget, error) { return b, nil }
func (fakeBudgets) Get(ctx context.Context, id string) (domain.Budget, error) {
	return domain.Budget{}, nil
}
func (fakeBudgets) List(ctx context.Context) ([]domain.Budget, error)         { return nil, nil }
func (fakeBudgets) Delete(ctx context.Context, id string) error               { return nil }
func (fakeBudgets) UpdateStatus(ctx context.Context, id, status string) error { return nil }
func (fakeBudgets) GetItem(ctx context.Context, budgetID, itemID string) (domain.BudgetItem, error) {
	return domain.BudgetItem{}, nil
}
func (fakeBudgets) UpdateItemNeededQty(ctx context.Context, itemID string, neededQty float64) (domain.BudgetItem, error) {
	return domain.BudgetItem{}, nil
}
func (fakeBudgets) DeleteItem(ctx context.Context, budgetID, itemID string) error { return nil }
func (fakeBudgets) ItemAllocatedQty(ctx context.Context, itemID string) (float64, error) {
	return 0, nil
}
func (fakeBudgets) AddAllocation(ctx context.Context, a domain.BudgetAllocation) (domain.BudgetAllocation, error) {
	return a, nil
}
func (fakeBudgets) DeleteAllocation(ctx context.Context, id string) error            { return nil }
func (fakeBudgets) SetAllocationQuote(ctx context.Context, id, quoteID string) error { return nil }

type fakeSchedules struct{}

func (fakeSchedules) Create(ctx context.Context, s domain.BudgetSchedule) (domain.BudgetSchedule, error) {
	return s, nil
}
func (fakeSchedules) Get(ctx context.Context, id string) (domain.BudgetSchedule, error) {
	return domain.BudgetSchedule{}, nil
}
func (fakeSchedules) List(ctx context.Context) ([]domain.BudgetSchedule, error) { return nil, nil }
func (fakeSchedules) Update(ctx context.Context, s domain.BudgetSchedule) (domain.BudgetSchedule, error) {
	return s, nil
}
func (fakeSchedules) Delete(ctx context.Context, id string) error { return nil }
func (fakeSchedules) Due(ctx context.Context, asOf time.Time) ([]domain.BudgetSchedule, error) {
	return nil, nil
}
func (fakeSchedules) MarkRun(ctx context.Context, id string, lastRun, nextRun time.Time) error {
	return nil
}

type fakeSeq struct{ n int64 }

func (s *fakeSeq) Next(ctx context.Context) (int64, error) { s.n++; return s.n, nil }
func (s *fakeSeq) EnsureMin(ctx context.Context, n int64) error {
	return nil
}

// --- test setup helpers ---

// kitScenario builds a catalog with one kit ("Cesta 3 pessoas", assembled from
// bananas), the banana raw product, and an unrelated standalone product
// ("Maçã", never part of any kit) sold directly — proving the kit fix doesn't
// disturb ordinary product forecasting. The kit's linked product is the one
// that shows up in real sales history, since a kit sells as its linked final
// product (see effectiveKitQty).
func kitScenario(kitSalesQty float64, kitOverrideQty *float64) *Service {
	assembly := domain.Assembly{
		ID:        "asm-cesta3",
		Code:      "000016",
		Name:      "Cesta 3 pessoas",
		ProductID: "prod-cesta3",
		Cost:      10,
		Items: []domain.AssemblyItem{
			{ProductID: "prod-banana", Quantity: 4.5},
		},
	}
	catalog := &fakeCatalog{
		products: []domain.Product{
			{ID: "prod-cesta3", SKU: "000016", Name: "Cesta 3 pessoas", Kind: "FINAL", SalePrice: 40},
			{ID: "prod-banana", SKU: "000001", Name: "Banana", Kind: "FINAL", PurchasePrice: 2},
			{ID: "prod-maca", SKU: "000002", Name: "Maçã", Kind: "FINAL", SalePrice: 5, PurchasePrice: 2},
		},
		assemblies: []domain.Assembly{assembly},
	}
	sales := &fakeSalesHistory{qty: map[string]float64{
		"prod-cesta3": kitSalesQty,
		"prod-maca":   6, // unrelated standalone product with its own real sales
	}}
	forecasts := &fakeForecastRepo{}
	if kitOverrideQty != nil {
		forecasts.overrides = append(forecasts.overrides, domain.ForecastOverride{
			Kind: domain.ForecastKindKit, TargetID: "asm-cesta3", WeeklyQty: *kitOverrideQty,
		})
	}
	return New(forecasts, fakeBudgets{}, fakeSchedules{}, fakeSupplierPrices{}, sales, catalog, &fakePurchasing{}, &fakeSeq{})
}

// --- tests ---

func TestStoragePlanExpandsKitIntoComponentsFromRealSales(t *testing.T) {
	// 2 lookback weeks, 8 units of the kit sold in that window -> 4/week.
	svc := kitScenario(8, nil)

	lines, err := svc.StoragePlan(context.Background(), 1, 0, 2)
	if err != nil {
		t.Fatalf("StoragePlan: %v", err)
	}

	for _, l := range lines {
		if l.ProductID == "prod-cesta3" {
			t.Fatalf("kit's own linked product must not appear in the storage plan, got line: %+v", l)
		}
	}

	banana := findLine(t, lines, "prod-banana")
	// 4/week kit qty * 4.5 banana per kit = 18/week -> needed at 1 week coverage, 0% safety.
	if banana.ForecastQty != 18 {
		t.Fatalf("banana ForecastQty = %v, want 18 (4 kits/week * 4.5 banana/kit)", banana.ForecastQty)
	}
	if banana.NeededQty != 18 {
		t.Fatalf("banana NeededQty = %v, want 18", banana.NeededQty)
	}
}

func TestStoragePlanKitOverrideWinsOverRealSales(t *testing.T) {
	override := 10.0
	svc := kitScenario(8, &override) // real sales say 4/week, override says 10/week

	lines, err := svc.StoragePlan(context.Background(), 1, 0, 2)
	if err != nil {
		t.Fatalf("StoragePlan: %v", err)
	}

	banana := findLine(t, lines, "prod-banana")
	if banana.ForecastQty != 45 {
		t.Fatalf("banana ForecastQty = %v, want 45 (override 10 kits/week * 4.5 banana/kit)", banana.ForecastQty)
	}
}

// TestStoragePlanExpandsLegacyProductOverrideOnKitProduct reproduces real
// dev-data found in bi_db: some kits' manual weekly forecast was saved as a
// PRODUCT-kind override on the kit's own linked product ID (target_id = the
// product, not the assembly), from before anything filtered a kit's product
// out of the plain product-forecast input. effectiveKitQty must still honor
// it — see its comment — or these kits' forecasts silently vanish.
func TestStoragePlanExpandsLegacyProductOverrideOnKitProduct(t *testing.T) {
	assembly := domain.Assembly{
		ID: "asm-cesta3", Code: "000016", Name: "Cesta 3 pessoas", ProductID: "prod-cesta3",
		Items: []domain.AssemblyItem{{ProductID: "prod-banana", Quantity: 4.5}},
	}
	catalog := &fakeCatalog{
		products: []domain.Product{
			{ID: "prod-cesta3", SKU: "000016", Name: "Cesta 3 pessoas", Kind: "FINAL"},
			{ID: "prod-banana", SKU: "000001", Name: "Banana"},
		},
		assemblies: []domain.Assembly{assembly},
	}
	forecasts := &fakeForecastRepo{overrides: []domain.ForecastOverride{
		// legacy: kind=PRODUCT, target_id=the kit's own linked product, no real sales at all.
		{Kind: domain.ForecastKindProduct, TargetID: "prod-cesta3", WeeklyQty: 4},
	}}
	svc := New(forecasts, fakeBudgets{}, fakeSchedules{}, fakeSupplierPrices{}, &fakeSalesHistory{}, catalog, &fakePurchasing{}, &fakeSeq{})

	lines, err := svc.StoragePlan(context.Background(), 1, 0, 2)
	if err != nil {
		t.Fatalf("StoragePlan: %v", err)
	}
	for _, l := range lines {
		if l.ProductID == "prod-cesta3" {
			t.Fatalf("kit's own linked product must not appear in the storage plan, got line: %+v", l)
		}
	}
	banana := findLine(t, lines, "prod-banana")
	if banana.ForecastQty != 18 {
		t.Fatalf("banana ForecastQty = %v, want 18 (legacy override 4/week * 4.5 banana/kit)", banana.ForecastQty)
	}

	entries, err := svc.ListForecasts(context.Background(), 2, false)
	if err != nil {
		t.Fatalf("ListForecasts: %v", err)
	}
	var kitEntry *domain.ForecastEntry
	for i := range entries {
		if entries[i].TargetID == "asm-cesta3" {
			kitEntry = &entries[i]
		}
		if entries[i].TargetID == "prod-cesta3" {
			t.Fatalf("kit's linked product must not appear as its own PRODUCT entry, got: %+v", entries[i])
		}
	}
	if kitEntry == nil || kitEntry.EffectiveWeeklyQty != 4 {
		t.Fatalf("expected a KIT entry with EffectiveWeeklyQty=4 from the legacy override, got: %+v", kitEntry)
	}
}

func TestStoragePlanNoKitSignalExpandsNothing(t *testing.T) {
	svc := kitScenario(0, nil) // never sold, no override

	lines, err := svc.StoragePlan(context.Background(), 1, 0, 2)
	if err != nil {
		t.Fatalf("StoragePlan: %v", err)
	}
	for _, l := range lines {
		if l.ProductID == "prod-banana" {
			t.Fatalf("banana should have no demand when the kit has neither sales nor an override, got: %+v", l)
		}
	}
}

func TestListForecastsMergesKitProductIntoKitEntryNotProduct(t *testing.T) {
	svc := kitScenario(8, nil)

	entries, err := svc.ListForecasts(context.Background(), 2, false)
	if err != nil {
		t.Fatalf("ListForecasts: %v", err)
	}

	var kitEntry, productEntry *domain.ForecastEntry
	for i := range entries {
		e := entries[i]
		if e.TargetID == "prod-cesta3" && e.Kind == domain.ForecastKindProduct {
			productEntry = &entries[i]
		}
		if e.TargetID == "asm-cesta3" && e.Kind == domain.ForecastKindKit {
			kitEntry = &entries[i]
		}
	}
	if productEntry != nil {
		t.Fatalf("kit's linked product must not appear as a PRODUCT forecast entry, got: %+v", *productEntry)
	}
	if kitEntry == nil {
		t.Fatalf("expected a KIT forecast entry for the kit's real sales history, got entries: %+v", entries)
	}
	if kitEntry.EffectiveWeeklyQty != 4 {
		t.Fatalf("kit EffectiveWeeklyQty = %v, want 4 (8 units / 2 weeks)", kitEntry.EffectiveWeeklyQty)
	}
	if kitEntry.Code != "000016" || kitEntry.Name != "Cesta 3 pessoas" {
		t.Fatalf("kit entry should use the assembly's own code/name, got: %+v", *kitEntry)
	}
}

func TestStoragePlanIncludesOrdinaryProductUnaffectedByKitFix(t *testing.T) {
	svc := kitScenario(8, nil)

	lines, err := svc.StoragePlan(context.Background(), 1, 0, 2)
	if err != nil {
		t.Fatalf("StoragePlan: %v", err)
	}

	maca := findLine(t, lines, "prod-maca")
	// 6 units / 2 lookback weeks = 3/week, 1 week coverage, 0% safety.
	if maca.ForecastQty != 3 {
		t.Fatalf("maca ForecastQty = %v, want 3 (unrelated product's own demand must be untouched)", maca.ForecastQty)
	}
}

func TestListForecastsIncludesOrdinaryProductAlongsideKit(t *testing.T) {
	svc := kitScenario(8, nil)

	entries, err := svc.ListForecasts(context.Background(), 2, false)
	if err != nil {
		t.Fatalf("ListForecasts: %v", err)
	}

	var maca *domain.ForecastEntry
	for i := range entries {
		if entries[i].TargetID == "prod-maca" {
			maca = &entries[i]
		}
	}
	if maca == nil {
		t.Fatalf("expected a PRODUCT entry for the unrelated product, got: %+v", entries)
	}
	if maca.Kind != domain.ForecastKindProduct {
		t.Fatalf("maca Kind = %q, want PRODUCT", maca.Kind)
	}
	if maca.EffectiveWeeklyQty != 3 {
		t.Fatalf("maca EffectiveWeeklyQty = %v, want 3", maca.EffectiveWeeklyQty)
	}
}

func TestListForecastsExcludesKitEntryWhenExcluded(t *testing.T) {
	svc := kitScenario(8, nil)
	svc.forecasts.(*fakeForecastRepo).exclusions = []domain.ForecastExclusion{
		{Kind: domain.ForecastKindKit, TargetID: "asm-cesta3"},
	}

	visible, err := svc.ListForecasts(context.Background(), 2, false)
	if err != nil {
		t.Fatalf("ListForecasts: %v", err)
	}
	for _, e := range visible {
		if e.TargetID == "asm-cesta3" {
			t.Fatalf("excluded kit entry should be hidden when includeExcluded=false, got: %+v", e)
		}
	}

	withExcluded, err := svc.ListForecasts(context.Background(), 2, true)
	if err != nil {
		t.Fatalf("ListForecasts: %v", err)
	}
	found := false
	for _, e := range withExcluded {
		if e.TargetID == "asm-cesta3" {
			found = true
			if !e.Excluded {
				t.Fatalf("kit entry should report Excluded=true, got: %+v", e)
			}
		}
	}
	if !found {
		t.Fatalf("excluded kit entry should still appear when includeExcluded=true")
	}
}

func TestListForecastsExcludesProductEntryWhenExcluded(t *testing.T) {
	svc := kitScenario(8, nil)
	svc.forecasts.(*fakeForecastRepo).exclusions = []domain.ForecastExclusion{
		{Kind: domain.ForecastKindProduct, TargetID: "prod-maca"},
	}

	visible, err := svc.ListForecasts(context.Background(), 2, false)
	if err != nil {
		t.Fatalf("ListForecasts: %v", err)
	}
	for _, e := range visible {
		if e.TargetID == "prod-maca" {
			t.Fatalf("excluded product entry should be hidden when includeExcluded=false, got: %+v", e)
		}
	}
}

func findLine(t *testing.T, lines []domain.StoragePlanLine, productID string) domain.StoragePlanLine {
	t.Helper()
	for _, l := range lines {
		if l.ProductID == productID {
			return l
		}
	}
	t.Fatalf("no storage plan line for product %q, got: %+v", productID, lines)
	return domain.StoragePlanLine{}
}
