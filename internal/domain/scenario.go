package domain

import (
	"context"
	"time"
)

const (
	ScenarioActionSet        = "SET"
	ScenarioActionRemove     = "REMOVE"
	ScenarioActionReset      = "RESET"
	ScenarioActionImportReal = "IMPORT_REAL"

	// scenarioHistoryLimit caps how many undo steps a scenario keeps.
	ScenarioHistoryLimit = 50
)

// Scenario is a named what-if sandbox: a set of products/kits with a simulated weekly
// sales quantity each. It never touches real sales history nor the official forecast
// overrides — the real (computed) average is always shown next to the simulated value.
type Scenario struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Notes         string    `json:"notes"`
	CoverageWeeks int       `json:"coverage_weeks"`
	SafetyPercent float64   `json:"safety_percent"`
	LookbackWeeks int       `json:"lookback_weeks"`
	LineCount     int       `json:"line_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ScenarioLine is one simulated product or kit. WeeklyQty nil means "follow the real
// average"; a value overrides it for this scenario only.
type ScenarioLine struct {
	Kind      string   `json:"kind"`
	TargetID  string   `json:"target_id"`
	WeeklyQty *float64 `json:"weekly_qty"`
}

type ScenarioHistoryEntry struct {
	ID     string
	Action string
	Detail string
	Before []ScenarioLine
	After  []ScenarioLine
	Undone bool
}

type ScenarioRepository interface {
	Create(ctx context.Context, s Scenario) (Scenario, error)
	Get(ctx context.Context, id string) (Scenario, error)
	List(ctx context.Context) ([]Scenario, error)
	Update(ctx context.Context, s Scenario) (Scenario, error)
	Delete(ctx context.Context, id string) error
	Lines(ctx context.Context, id string) ([]ScenarioLine, error)
	ReplaceLines(ctx context.Context, id string, lines []ScenarioLine) error
	// PushHistory records a change and drops any redo tail (entries already undone).
	PushHistory(ctx context.Context, id string, e ScenarioHistoryEntry) error
	// LastApplied is the newest history entry not undone; NextUndone is the oldest undone one
	// (the next to redo). Both return nil when there is none.
	LastApplied(ctx context.Context, id string) (*ScenarioHistoryEntry, error)
	NextUndone(ctx context.Context, id string) (*ScenarioHistoryEntry, error)
	SetHistoryUndone(ctx context.Context, entryID string, undone bool) error
}

// ScenarioLineResult is one simulated line with its real reference and weekly economics.
type ScenarioLineResult struct {
	Kind               string   `json:"kind"`
	TargetID           string   `json:"target_id"`
	Code               string   `json:"code,omitempty"`
	Name               string   `json:"name,omitempty"`
	UoM                string   `json:"uom,omitempty"`
	RealWeeklyQty      float64  `json:"real_weekly_qty"`
	SimWeeklyQty       *float64 `json:"sim_weekly_qty"`
	EffectiveWeeklyQty float64  `json:"effective_weekly_qty"`
	DeltaQty           float64  `json:"delta_qty"`
	DeltaPercent       *float64 `json:"delta_percent,omitempty"`
	UnitRevenue        float64  `json:"unit_revenue"`
	UnitCost           float64  `json:"unit_cost"`
	WeeklyRevenue      float64  `json:"weekly_revenue"`
	WeeklyCost         float64  `json:"weekly_cost"`
	WeeklyProfit       float64  `json:"weekly_profit"`
	MarginPercent      float64  `json:"margin_percent"`
}

// ScenarioStockLine is the stock projection for one purchasable product, after expanding
// kits into their components.
type ScenarioStockLine struct {
	ProductID    string  `json:"product_id"`
	SKU          string  `json:"sku,omitempty"`
	Name         string  `json:"name,omitempty"`
	UoM          string  `json:"uom,omitempty"`
	WeeklyDemand float64 `json:"weekly_demand"`
	TargetStock  float64 `json:"target_stock"`
	OnHandQty    float64 `json:"on_hand_qty"`
	OpenPOQty    float64 `json:"open_po_qty"`
	ToBuyQty     float64 `json:"to_buy_qty"`
	UnitCost     float64 `json:"unit_cost"`
	PurchaseCost float64 `json:"purchase_cost"`
	StockValue   float64 `json:"stock_value"`
}

type ScenarioSummary struct {
	WeeklyRevenue   float64 `json:"weekly_revenue"`
	WeeklyCost      float64 `json:"weekly_cost"`
	WeeklyProfit    float64 `json:"weekly_profit"`
	MarginPercent   float64 `json:"margin_percent"`
	CoverageRevenue float64 `json:"coverage_revenue"`
	PurchaseCost    float64 `json:"purchase_cost"`
	StockValue      float64 `json:"stock_value"`
}

type ScenarioResult struct {
	Scenario   Scenario             `json:"scenario"`
	Lines      []ScenarioLineResult `json:"lines"`
	Stock      []ScenarioStockLine  `json:"stock"`
	Summary    ScenarioSummary      `json:"summary"`
	CanUndo    bool                 `json:"can_undo"`
	UndoAction string               `json:"undo_action,omitempty"`
	UndoDetail string               `json:"undo_detail,omitempty"`
	CanRedo    bool                 `json:"can_redo"`
	RedoAction string               `json:"redo_action,omitempty"`
	RedoDetail string               `json:"redo_detail,omitempty"`
}

// StockUnitCost is the cost per stock-UoM unit used to price purchases: the cheapest
// registered supplier price when there is one, otherwise the product's own purchase price
// converted from its purchase UoM into the stock UoM.
func StockUnitCost(p Product, supplierUnitPrice float64, hasSupplier bool) float64 {
	if hasSupplier && supplierUnitPrice > 0 {
		return supplierUnitPrice
	}
	stockUoM := p.StockUnitOfMeasure()
	if p.PurchaseUoM != "" && p.PurchaseUoM != stockUoM {
		if per, ok := ConvertQty(1, p.PurchaseUoM, stockUoM, p.Conversions); ok && per > 0 {
			return p.PurchasePrice / per
		}
	}
	return p.PurchasePrice
}
