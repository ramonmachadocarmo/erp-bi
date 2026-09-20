package application

import (
	"context"
	"math"
	"testing"

	"erp/services/bi-service/internal/domain"
)

type fakeScenarioRepo struct {
	scenarios map[string]domain.Scenario
	lines     map[string][]domain.ScenarioLine
	history   map[string][]*domain.ScenarioHistoryEntry
	next      int
}

func newFakeScenarioRepo() *fakeScenarioRepo {
	return &fakeScenarioRepo{
		scenarios: map[string]domain.Scenario{},
		lines:     map[string][]domain.ScenarioLine{},
		history:   map[string][]*domain.ScenarioHistoryEntry{},
	}
}

func (f *fakeScenarioRepo) Create(_ context.Context, s domain.Scenario) (domain.Scenario, error) {
	f.next++
	s.ID = "sc" + string(rune('0'+f.next))
	f.scenarios[s.ID] = s
	return s, nil
}
func (f *fakeScenarioRepo) Get(_ context.Context, id string) (domain.Scenario, error) {
	s, ok := f.scenarios[id]
	if !ok {
		return domain.Scenario{}, domain.ErrNotFound
	}
	s.LineCount = len(f.lines[id])
	return s, nil
}
func (f *fakeScenarioRepo) List(context.Context) ([]domain.Scenario, error) { return nil, nil }
func (f *fakeScenarioRepo) Update(_ context.Context, s domain.Scenario) (domain.Scenario, error) {
	f.scenarios[s.ID] = s
	return s, nil
}
func (f *fakeScenarioRepo) Delete(_ context.Context, id string) error {
	delete(f.scenarios, id)
	return nil
}
func (f *fakeScenarioRepo) Lines(_ context.Context, id string) ([]domain.ScenarioLine, error) {
	return copyLines(f.lines[id]), nil
}
func (f *fakeScenarioRepo) ReplaceLines(_ context.Context, id string, lines []domain.ScenarioLine) error {
	f.lines[id] = copyLines(lines)
	return nil
}
func (f *fakeScenarioRepo) PushHistory(_ context.Context, id string, e domain.ScenarioHistoryEntry) error {
	kept := f.history[id][:0]
	for _, h := range f.history[id] {
		if !h.Undone {
			kept = append(kept, h)
		}
	}
	f.next++
	e.ID = "h" + string(rune('0'+f.next))
	f.history[id] = append(kept, &e)
	return nil
}
func (f *fakeScenarioRepo) LastApplied(_ context.Context, id string) (*domain.ScenarioHistoryEntry, error) {
	h := f.history[id]
	for i := len(h) - 1; i >= 0; i-- {
		if !h[i].Undone {
			return h[i], nil
		}
	}
	return nil, nil
}
func (f *fakeScenarioRepo) NextUndone(_ context.Context, id string) (*domain.ScenarioHistoryEntry, error) {
	for _, h := range f.history[id] {
		if h.Undone {
			return h, nil
		}
	}
	return nil, nil
}
func (f *fakeScenarioRepo) SetHistoryUndone(_ context.Context, entryID string, undone bool) error {
	for _, hs := range f.history {
		for _, h := range hs {
			if h.ID == entryID {
				h.Undone = undone
			}
		}
	}
	return nil
}

func scenarioSvc() (*Service, *fakeForecastRepo) {
	forecasts := &fakeForecastRepo{}
	catalog := &fakeCatalog{
		products: []domain.Product{
			{ID: "apple", SKU: "000001", Name: "Maçã", SalePrice: 10, PurchasePrice: 6, StockUoM: "KG", SaleUoM: "KG", PurchaseUoM: "KG"},
			{ID: "bag", SKU: "000002", Name: "Sacola", SalePrice: 0, PurchasePrice: 2, StockUoM: "UN", SaleUoM: "UN", PurchaseUoM: "UN"},
			{ID: "kitprod", SKU: "000003", Name: "Kit Frutas", SalePrice: 40, StockUoM: "UN", SaleUoM: "UN"},
		},
		assemblies: []domain.Assembly{
			{ID: "kit1", Code: "000001", Name: "Frutas", ProductID: "kitprod", Cost: 14, Items: []domain.AssemblyItem{{ProductID: "apple", Quantity: 2}, {ProductID: "bag", Quantity: 1}}},
		},
		balances: []domain.Balance{{ProductID: "apple", QuantityAvailable: 5}},
	}
	// 16 sold in an 8-week window = real average 2/week of apples; kit product sold 8 = 1/week.
	sales := &fakeSalesHistory{qty: map[string]float64{"apple": 16, "kitprod": 8}}
	svc := New(forecasts, fakeBudgets{}, fakeSchedules{}, fakeSupplierPrices{}, sales, catalog, &fakePurchasing{}, &fakeSeq{})
	return svc.WithScenarios(newFakeScenarioRepo()), forecasts
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestScenarioKeepsRealReferenceAndComputesProjection(t *testing.T) {
	svc, forecasts := scenarioSvc()
	ctx := context.Background()
	sc, err := svc.CreateScenario(ctx, domain.Scenario{Name: "Base", CoverageWeeks: 2, SafetyPercent: 0, LookbackWeeks: 8})
	if err != nil {
		t.Fatal(err)
	}
	four := 4.0
	if _, err := svc.SetScenarioLine(ctx, sc.ID, domain.ForecastKindKit, "kit1", &four); err != nil {
		t.Fatal(err)
	}
	res, err := svc.SetScenarioLine(ctx, sc.ID, domain.ForecastKindProduct, "apple", nil)
	if err != nil {
		t.Fatal(err)
	}
	var kit, apple *domain.ScenarioLineResult
	for i := range res.Lines {
		switch res.Lines[i].TargetID {
		case "kit1":
			kit = &res.Lines[i]
		case "apple":
			apple = &res.Lines[i]
		}
	}
	// Real average stays visible next to the simulated value.
	if kit == nil || !approx(kit.RealWeeklyQty, 1) || !approx(kit.EffectiveWeeklyQty, 4) || !approx(kit.DeltaQty, 3) {
		t.Fatalf("kit line: %+v", kit)
	}
	if kit.DeltaPercent == nil || !approx(*kit.DeltaPercent, 300) {
		t.Fatalf("delta percent: %+v", kit.DeltaPercent)
	}
	// A line with no simulated value follows the real average.
	if apple == nil || apple.SimWeeklyQty != nil || !approx(apple.EffectiveWeeklyQty, 2) {
		t.Fatalf("apple line: %+v", apple)
	}
	// Revenue: kit 4 × 40 + apple 2 × 10 = 180; cost: kit 4 × 14 + apple 2 × 6 = 68.
	if !approx(res.Summary.WeeklyRevenue, 180) || !approx(res.Summary.WeeklyCost, 68) {
		t.Fatalf("summary: %+v", res.Summary)
	}
	// Demand: apple = 4×2 (kit) + 2 (direct) = 10/wk → target 20 for 2 weeks, on hand 5 → buy 15; bag = 4/wk → 8.
	stock := map[string]domain.ScenarioStockLine{}
	for _, s := range res.Stock {
		stock[s.ProductID] = s
	}
	if !approx(stock["apple"].WeeklyDemand, 10) || !approx(stock["apple"].TargetStock, 20) || !approx(stock["apple"].ToBuyQty, 15) {
		t.Fatalf("apple stock: %+v", stock["apple"])
	}
	if !approx(stock["bag"].ToBuyQty, 8) || !approx(res.Summary.PurchaseCost, 15*6+8*2) {
		t.Fatalf("bag stock / purchase cost: %+v %v", stock["bag"], res.Summary.PurchaseCost)
	}
	if len(forecasts.overrides) != 0 {
		t.Fatalf("scenario must not touch official forecast overrides: %+v", forecasts.overrides)
	}
}

func TestScenarioUndoRedoAndResetToReal(t *testing.T) {
	svc, _ := scenarioSvc()
	ctx := context.Background()
	sc, _ := svc.CreateScenario(ctx, domain.Scenario{Name: "A"})
	one, five := 1.0, 5.0
	svc.SetScenarioLine(ctx, sc.ID, domain.ForecastKindProduct, "apple", &one)
	res, _ := svc.SetScenarioLine(ctx, sc.ID, domain.ForecastKindProduct, "apple", &five)
	if !res.CanUndo || res.CanRedo || !approx(res.Lines[0].EffectiveWeeklyQty, 5) {
		t.Fatalf("%+v", res)
	}

	res, err := svc.UndoScenario(ctx, sc.ID)
	if err != nil || !approx(res.Lines[0].EffectiveWeeklyQty, 1) || !res.CanRedo {
		t.Fatalf("undo: %v %+v", err, res.Lines)
	}
	res, err = svc.RedoScenario(ctx, sc.ID)
	if err != nil || !approx(res.Lines[0].EffectiveWeeklyQty, 5) || res.CanRedo {
		t.Fatalf("redo: %v %+v", err, res.Lines)
	}

	// Back to real for every line, and that itself can be undone.
	res, _ = svc.ResetScenarioToReal(ctx, sc.ID)
	if res.Lines[0].SimWeeklyQty != nil || !approx(res.Lines[0].EffectiveWeeklyQty, 2) {
		t.Fatalf("reset: %+v", res.Lines[0])
	}
	res, _ = svc.UndoScenario(ctx, sc.ID)
	if !approx(res.Lines[0].EffectiveWeeklyQty, 5) {
		t.Fatalf("undo reset: %+v", res.Lines[0])
	}

	// A new change after an undo drops the redo tail.
	svc.UndoScenario(ctx, sc.ID)
	res, _ = svc.SetScenarioLine(ctx, sc.ID, domain.ForecastKindProduct, "apple", &one)
	if res.CanRedo {
		t.Fatal("new change should clear redo")
	}
}

func TestScenarioLineValidationAndImportReal(t *testing.T) {
	svc, _ := scenarioSvc()
	ctx := context.Background()
	sc, _ := svc.CreateScenario(ctx, domain.Scenario{Name: "A"})
	neg := -1.0
	if _, err := svc.SetScenarioLine(ctx, sc.ID, domain.ForecastKindProduct, "apple", &neg); err != domain.ErrInvalid {
		t.Fatalf("negative qty: %v", err)
	}
	// A kit's own linked product must be simulated as the kit, not as a product.
	if _, err := svc.SetScenarioLine(ctx, sc.ID, domain.ForecastKindProduct, "kitprod", nil); err != domain.ErrInvalid {
		t.Fatalf("kit product as PRODUCT: %v", err)
	}
	res, err := svc.ImportRealLines(ctx, sc.ID)
	if err != nil || len(res.Lines) != 2 {
		t.Fatalf("import: %v %+v", err, res.Lines)
	}
	res, _ = svc.RemoveScenarioLine(ctx, sc.ID, domain.ForecastKindKit, "kit1")
	if len(res.Lines) != 1 {
		t.Fatalf("remove: %+v", res.Lines)
	}
	dup, err := svc.DuplicateScenario(ctx, sc.ID, "")
	if err != nil || dup.LineCount != 1 || dup.Name != "A (cópia)" {
		t.Fatalf("duplicate: %v %+v", err, dup)
	}
}
