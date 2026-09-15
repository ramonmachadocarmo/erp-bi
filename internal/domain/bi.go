package domain

import (
	"context"
	"errors"
	"math"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
	ErrInvalid  = errors.New("invalid")
	ErrInUse    = errors.New("cannot delete: record is in use")
)

const (
	BudgetStatusDraft     = "DRAFT"
	BudgetStatusAllocated = "ALLOCATED"
	BudgetStatusConfirmed = "CONFIRMED"
	BudgetStatusCancelled = "CANCELLED"

	ScheduleFrequencyWeekly  = "WEEKLY"
	ScheduleFrequencyMonthly = "MONTHLY"

	ForecastKindProduct = "PRODUCT"
	ForecastKindKit     = "KIT"

	scheduleRunHourUTC = 3
)

// ForecastOverride is a manually-set weekly quantity for either a product or
// a kit (assembly). Kit overrides have no computed/moving-average signal of
// their own — they exist purely to be expanded into component product demand
// (see Service.effectiveForecasts) — while product overrides sit alongside a
// computed value and win over it when present.
type ForecastOverride struct {
	Kind      string    `json:"kind"`
	TargetID  string    `json:"target_id"`
	WeeklyQty float64   `json:"weekly_qty"`
	Note      string    `json:"note,omitempty"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ForecastEntry struct {
	Kind               string   `json:"kind"`
	TargetID           string   `json:"target_id"`
	Code               string   `json:"code,omitempty"`
	Name               string   `json:"name,omitempty"`
	ComputedWeeklyQty  float64  `json:"computed_weekly_qty"`
	OverrideWeeklyQty  *float64 `json:"override_weekly_qty,omitempty"`
	EffectiveWeeklyQty float64  `json:"effective_weekly_qty"`
	LookbackWeeks      int      `json:"lookback_weeks"`
	Excluded           bool     `json:"excluded"`
}

// FinancialLine is one forecasted product or kit's projected weekly revenue,
// cost, and profit — the "Receita x Despesa" screen's row. UnitRevenue is the
// product's (or, for a kit, the kit's own linked product's) sale price;
// UnitCost is the product's purchase price, or for a kit, the assembly's
// full cost (every component/support item's purchase price × its quantity
// per kit — see FinancialLineFor).
type FinancialLine struct {
	Kind          string  `json:"kind"`
	TargetID      string  `json:"target_id"`
	Code          string  `json:"code,omitempty"`
	Name          string  `json:"name,omitempty"`
	WeeklyQty     float64 `json:"weekly_qty"`
	UnitRevenue   float64 `json:"unit_revenue"`
	UnitCost      float64 `json:"unit_cost"`
	WeeklyRevenue float64 `json:"weekly_revenue"`
	WeeklyCost    float64 `json:"weekly_cost"`
	WeeklyProfit  float64 `json:"weekly_profit"`
	MarginPercent float64 `json:"margin_percent"`
}

// FinancialLineFor turns a forecast entry plus its unit economics into a
// projected weekly P&L line. MarginPercent is 0 when there's no revenue to
// divide by (e.g. an item with cost but not currently priced for sale).
func FinancialLineFor(f ForecastEntry, unitRevenue, unitCost float64) FinancialLine {
	weeklyRevenue := f.EffectiveWeeklyQty * unitRevenue
	weeklyCost := f.EffectiveWeeklyQty * unitCost
	weeklyProfit := weeklyRevenue - weeklyCost
	margin := 0.0
	if weeklyRevenue > 0 {
		margin = weeklyProfit / weeklyRevenue * 100
	}
	return FinancialLine{
		Kind: f.Kind, TargetID: f.TargetID, Code: f.Code, Name: f.Name,
		WeeklyQty: f.EffectiveWeeklyQty, UnitRevenue: unitRevenue, UnitCost: unitCost,
		WeeklyRevenue: weeklyRevenue, WeeklyCost: weeklyCost, WeeklyProfit: weeklyProfit, MarginPercent: margin,
	}
}

// ForecastExclusion hides a product or kit from the forecast list without
// touching its sales history or override — e.g. a discontinued item that
// still has recent sales but shouldn't keep cluttering the forecast screen.
// It does not affect storage-plan/budget demand, which still accounts for
// the item's real sales history and any override.
type ForecastExclusion struct {
	Kind       string    `json:"kind"`
	TargetID   string    `json:"target_id"`
	ExcludedAt time.Time `json:"excluded_at"`
}

type StoragePlanLine struct {
	ProductID   string  `json:"product_id"`
	SKU         string  `json:"sku,omitempty"`
	Name        string  `json:"name,omitempty"`
	ForecastQty float64 `json:"forecast_qty"`
	OnHandQty   float64 `json:"on_hand_qty"`
	OpenPOQty   float64 `json:"open_po_qty"`
	NeededQty   float64 `json:"needed_qty"`
}

type Budget struct {
	ID            string       `json:"id"`
	Code          string       `json:"code"`
	Status        string       `json:"status"`
	CoverageWeeks int          `json:"coverage_weeks"`
	SafetyPercent float64      `json:"safety_percent"`
	LookbackWeeks int          `json:"lookback_weeks"`
	ScheduleID    *string      `json:"schedule_id,omitempty"`
	Items         []BudgetItem `json:"items"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type BudgetItem struct {
	ID          string             `json:"id"`
	BudgetID    string             `json:"budget_id,omitempty"`
	ProductID   string             `json:"product_id"`
	ForecastQty float64            `json:"forecast_qty"`
	OnHandQty   float64            `json:"on_hand_qty"`
	OpenPOQty   float64            `json:"open_po_qty"`
	NeededQty   float64            `json:"needed_qty"`
	Allocations []BudgetAllocation `json:"allocations,omitempty"`
}

type BudgetAllocation struct {
	ID           string    `json:"id"`
	BudgetItemID string    `json:"budget_item_id,omitempty"`
	SupplierID   string    `json:"supplier_id"`
	Quantity     float64   `json:"quantity"`
	UnitPrice    float64   `json:"unit_price"`
	QuoteID      *string   `json:"quote_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type BudgetSchedule struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Frequency     string     `json:"frequency"`
	DayOfWeek     *int       `json:"day_of_week,omitempty"`
	DayOfMonth    *int       `json:"day_of_month,omitempty"`
	CoverageWeeks int        `json:"coverage_weeks"`
	SafetyPercent float64    `json:"safety_percent"`
	LookbackWeeks int        `json:"lookback_weeks"`
	Active        bool       `json:"active"`
	NextRunAt     time.Time  `json:"next_run_at"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// --- pure functions ---

func MovingAverageWeekly(totalQtyInWindow float64, lookbackWeeks int) float64 {
	if lookbackWeeks <= 0 {
		return 0
	}
	return totalQtyInWindow / float64(lookbackWeeks)
}

func EffectiveWeeklyQty(computed float64, override *float64) float64 {
	if override != nil {
		return *override
	}
	return computed
}

func NeededQty(effectiveWeeklyQty float64, coverageWeeks int, safetyPercent, onHandQty, openPOQty float64) float64 {
	raw := effectiveWeeklyQty * float64(coverageWeeks) * (1 + safetyPercent/100)
	// Subtract a small epsilon before ceiling so float64 rounding error (e.g.
	// 200*1.1 = 220.00000000000003) doesn't push an exact result up a whole unit.
	needed := math.Ceil(raw-1e-9) - onHandQty - openPOQty
	if needed < 0 {
		return 0
	}
	return needed
}

// FirstRunAt computes the next scheduled run at or after now, for a schedule
// created/edited "right now". WEEKLY runs on dayOfWeek (0=Sunday..6=Saturday);
// MONTHLY runs on dayOfMonth (clamped to the last real day of the month).
func FirstRunAt(freq string, dayOfWeek, dayOfMonth *int, now time.Time) time.Time {
	now = now.UTC()
	switch freq {
	case ScheduleFrequencyWeekly:
		dow := 0
		if dayOfWeek != nil {
			dow = *dayOfWeek
		}
		t := time.Date(now.Year(), now.Month(), now.Day(), scheduleRunHourUTC, 0, 0, 0, time.UTC)
		for int(t.Weekday()) != dow || !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		return t
	case ScheduleFrequencyMonthly:
		dom := 1
		if dayOfMonth != nil {
			dom = *dayOfMonth
		}
		t := clampToMonthDay(now.Year(), int(now.Month()), dom, scheduleRunHourUTC)
		if !t.After(now) {
			t = clampToMonthDay(now.Year(), int(now.Month())+1, dom, scheduleRunHourUTC)
		}
		return t
	default:
		return now.AddDate(0, 0, 7)
	}
}

// AdvanceRunAt computes the next run after prev succeeded.
func AdvanceRunAt(freq string, dayOfMonth *int, prev time.Time) time.Time {
	switch freq {
	case ScheduleFrequencyWeekly:
		return prev.AddDate(0, 0, 7)
	case ScheduleFrequencyMonthly:
		dom := prev.Day()
		if dayOfMonth != nil {
			dom = *dayOfMonth
		}
		return clampToMonthDay(prev.Year(), int(prev.Month())+1, dom, prev.Hour())
	default:
		return prev.AddDate(0, 0, 7)
	}
}

// clampToMonthDay builds a UTC timestamp for (year, month, day, hour), clamping day
// to the last day of that month when day exceeds it (e.g. day_of_month=31 in Feb -> 28/29).
func clampToMonthDay(year, month, day, hour int) time.Time {
	for month > 12 {
		month -= 12
		year++
	}
	for month < 1 {
		month += 12
		year--
	}
	firstOfNext := time.Date(year, time.Month(month)+1, 1, 0, 0, 0, 0, time.UTC)
	daysInMonth := firstOfNext.AddDate(0, 0, -1).Day()
	if day > daysInMonth {
		day = daysInMonth
	}
	if day < 1 {
		day = 1
	}
	return time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC)
}

// --- ports ---

type ForecastRepository interface {
	ListOverrides(ctx context.Context) ([]ForecastOverride, error)
	SetOverride(ctx context.Context, o ForecastOverride) (ForecastOverride, error)
	DeleteOverride(ctx context.Context, kind, targetID string) error
	ListExclusions(ctx context.Context) ([]ForecastExclusion, error)
	SetExclusion(ctx context.Context, kind, targetID string) error
	DeleteExclusion(ctx context.Context, kind, targetID string) error
}

type SupplierPriceRepository interface {
	List(ctx context.Context) ([]SupplierPrice, error)
	Upsert(ctx context.Context, sp SupplierPrice) (SupplierPrice, error)
	Delete(ctx context.Context, id string) error
}

type BudgetRepository interface {
	Create(ctx context.Context, b Budget) (Budget, error)
	Get(ctx context.Context, id string) (Budget, error)
	List(ctx context.Context) ([]Budget, error)
	Delete(ctx context.Context, id string) error
	UpdateStatus(ctx context.Context, id, status string) error
	GetItem(ctx context.Context, budgetID, itemID string) (BudgetItem, error)
	UpdateItemNeededQty(ctx context.Context, itemID string, neededQty float64) (BudgetItem, error)
	DeleteItem(ctx context.Context, budgetID, itemID string) error
	ItemAllocatedQty(ctx context.Context, itemID string) (float64, error)
	AddAllocation(ctx context.Context, a BudgetAllocation) (BudgetAllocation, error)
	DeleteAllocation(ctx context.Context, id string) error
	SetAllocationQuote(ctx context.Context, id, quoteID string) error
}

type ScheduleRepository interface {
	Create(ctx context.Context, s BudgetSchedule) (BudgetSchedule, error)
	Get(ctx context.Context, id string) (BudgetSchedule, error)
	List(ctx context.Context) ([]BudgetSchedule, error)
	Update(ctx context.Context, s BudgetSchedule) (BudgetSchedule, error)
	Delete(ctx context.Context, id string) error
	Due(ctx context.Context, asOf time.Time) ([]BudgetSchedule, error)
	MarkRun(ctx context.Context, id string, lastRun, nextRun time.Time) error
}

// SupplierPrice records what a supplier charges for a product, so the
// Orçamentos allocation screen can suggest a price instead of starting blank.
// SupplierPrice records what a supplier charges for MinQty units of a
// product's purchase unit of measure (e.g. "$12 for 1.5 CX") — not
// necessarily $/unit, since suppliers quote in whatever pack size they sell.
type SupplierPrice struct {
	ID         string    `json:"id"`
	ProductID  string    `json:"product_id"`
	SupplierID string    `json:"supplier_id"`
	Price      float64   `json:"price"`
	MinQty     float64   `json:"min_qty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// --- peer-service ports ---

type UoMConversion struct {
	FromUoM string
	ToUoM   string
	Factor  float64
}

// ConvertQty converts a quantity between two units of measure using the
// registered conversions (checked in either direction). ok is false when
// from/to genuinely differ and no matching factor is registered — callers
// must not guess a conversion in that case.
func ConvertQty(qty float64, from, to string, convs []UoMConversion) (float64, bool) {
	if from == "" || to == "" || from == to {
		return qty, true
	}
	for _, c := range convs {
		if c.Factor <= 0 {
			continue
		}
		if c.FromUoM == from && c.ToUoM == to {
			return qty * c.Factor, true
		}
		if c.FromUoM == to && c.ToUoM == from {
			return qty / c.Factor, true
		}
	}
	return 0, false
}

// PurchaseLine converts a quantity needed in the product's stock UoM into a
// real purchase-order line: how many purchase-UoM units to actually order,
// and the price per purchase-UoM unit. Suppliers often sell in fixed pack
// sizes (e.g. boxes of 20kg only, never loose kg) — ordering exactly the
// needed quantity usually isn't possible, so when a registered SupplierPrice
// is known this rounds UP to the next whole pack (price.MinQty units of
// purchase UoM). Without a registered price, it rounds up to whole
// purchase-UoM units instead (no pack size to align to) and falls back to
// unitCost (converted into a per-purchase-unit price) or, failing that, the
// product's own purchase price. ok is false only when the purchase UoM can't
// be converted to the stock UoM at all.
func PurchaseLine(stockQty float64, p Product, price *SupplierPrice, unitCost float64) (purchaseQty, unitPrice float64, ok bool) {
	stockUoM := p.StockUnitOfMeasure()
	if p.PurchaseUoM == "" || p.PurchaseUoM == stockUoM {
		unitPrice = unitCost
		if price != nil && price.MinQty > 0 {
			unitPrice = price.Price / price.MinQty
		} else if unitPrice <= 0 {
			unitPrice = p.PurchasePrice
		}
		return stockQty, unitPrice, true
	}
	stockPerPurchaseUnit, convOK := ConvertQty(1, p.PurchaseUoM, stockUoM, p.Conversions)
	if !convOK || stockPerPurchaseUnit <= 0 {
		return 0, 0, false
	}
	if price != nil && price.MinQty > 0 {
		packStockQty := price.MinQty * stockPerPurchaseUnit
		packs := math.Ceil(stockQty/packStockQty - 1e-9)
		if packs < 1 {
			packs = 1
		}
		return packs * price.MinQty, price.Price / price.MinQty, true
	}
	purchaseQty = math.Ceil(stockQty/stockPerPurchaseUnit - 1e-9)
	if purchaseQty < 1 {
		purchaseQty = 1
	}
	unitPrice = unitCost * stockPerPurchaseUnit
	if unitPrice <= 0 {
		unitPrice = p.PurchasePrice
	}
	return purchaseQty, unitPrice, true
}

type Product struct {
	ID            string
	SKU           string
	Name          string
	Kind          string
	PurchasePrice float64
	SalePrice     float64
	PurchaseUoM   string
	StockUoM      string
	SaleUoM       string
	Conversions   []UoMConversion
}

// StockUnitOfMeasure is the unit item quantities (needed_qty, balances, sales
// history) are expressed in — mirrors the frontend's stockUom() helper.
func (p Product) StockUnitOfMeasure() string {
	if p.StockUoM != "" {
		return p.StockUoM
	}
	return p.SaleUoM
}

type Balance struct {
	ProductID         string
	WarehouseID       string
	QuantityAvailable float64
}

type AssemblyItem struct {
	ProductID string
	Quantity  float64
}

type Assembly struct {
	ID        string
	Code      string
	Name      string
	ProductID string
	Cost      float64
	Items     []AssemblyItem
}

type Catalog interface {
	Products(ctx context.Context) ([]Product, error)
	Balances(ctx context.Context) ([]Balance, error)
	Assemblies(ctx context.Context) ([]Assembly, error)
}

type SalesHistory interface {
	OrderQuantities(ctx context.Context, from, to time.Time) (map[string]float64, error)
}

type QuoteItem struct {
	ProductID string
	Quantity  float64
	UnitPrice float64
}

type Purchasing interface {
	OpenOrderQuantities(ctx context.Context) (map[string]float64, error)
	CreateQuote(ctx context.Context, supplierID string, items []QuoteItem, notes string) (quoteID string, err error)
}
