package application

import (
	"context"
	"math"
	"sort"
	"strings"

	"erp/services/bi-service/internal/domain"
)

// WithScenarios wires the what-if scenario store (kept out of New so the core forecasting
// service doesn't depend on it).
func (s *Service) WithScenarios(r domain.ScenarioRepository) *Service {
	s.scenarios = r
	return s
}

func (s *Service) CreateScenario(ctx context.Context, sc domain.Scenario) (domain.Scenario, error) {
	sc.Name = strings.TrimSpace(sc.Name)
	if sc.Name == "" {
		return domain.Scenario{}, domain.ErrInvalid
	}
	if sc.CoverageWeeks <= 0 {
		sc.CoverageWeeks = 4
	}
	if sc.LookbackWeeks <= 0 {
		sc.LookbackWeeks = 8
	}
	if sc.SafetyPercent < 0 {
		return domain.Scenario{}, domain.ErrInvalid
	}
	return s.scenarios.Create(ctx, sc)
}

func (s *Service) ListScenarios(ctx context.Context) ([]domain.Scenario, error) {
	return s.scenarios.List(ctx)
}

func (s *Service) UpdateScenario(ctx context.Context, id string, in domain.Scenario) (domain.ScenarioResult, error) {
	cur, err := s.scenarios.Get(ctx, id)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		cur.Name = name
	}
	cur.Notes = in.Notes
	if in.CoverageWeeks > 0 {
		cur.CoverageWeeks = in.CoverageWeeks
	}
	if in.LookbackWeeks > 0 {
		cur.LookbackWeeks = in.LookbackWeeks
	}
	if in.SafetyPercent >= 0 {
		cur.SafetyPercent = in.SafetyPercent
	}
	if _, err := s.scenarios.Update(ctx, cur); err != nil {
		return domain.ScenarioResult{}, err
	}
	return s.ScenarioResult(ctx, id)
}

func (s *Service) DeleteScenario(ctx context.Context, id string) error {
	return s.scenarios.Delete(ctx, id)
}

// DuplicateScenario copies a scenario's parameters and lines under a new name (no history),
// so it can be tweaked without risking the original.
func (s *Service) DuplicateScenario(ctx context.Context, id, name string) (domain.Scenario, error) {
	src, err := s.scenarios.Get(ctx, id)
	if err != nil {
		return domain.Scenario{}, err
	}
	lines, err := s.scenarios.Lines(ctx, id)
	if err != nil {
		return domain.Scenario{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = src.Name + " (cópia)"
	}
	dup, err := s.scenarios.Create(ctx, domain.Scenario{
		Name: name, Notes: src.Notes, CoverageWeeks: src.CoverageWeeks, SafetyPercent: src.SafetyPercent, LookbackWeeks: src.LookbackWeeks,
	})
	if err != nil {
		return domain.Scenario{}, err
	}
	if err := s.scenarios.ReplaceLines(ctx, dup.ID, lines); err != nil {
		return domain.Scenario{}, err
	}
	return s.scenarios.Get(ctx, dup.ID)
}

func validScenarioKind(kind string) bool {
	return kind == domain.ForecastKindProduct || kind == domain.ForecastKindKit
}

func copyLines(lines []domain.ScenarioLine) []domain.ScenarioLine {
	out := make([]domain.ScenarioLine, len(lines))
	for i, l := range lines {
		out[i] = l
		if l.WeeklyQty != nil {
			v := *l.WeeklyQty
			out[i].WeeklyQty = &v
		}
	}
	return out
}

func (s *Service) targetLabel(ctx context.Context, kind, targetID string) string {
	if kind == domain.ForecastKindKit {
		if as, err := s.assemblyLookup(ctx); err == nil {
			if a, ok := as[targetID]; ok {
				return strings.TrimSpace(a.Code + " " + a.Name)
			}
		}
		return targetID
	}
	if ps, err := s.productLookup(ctx); err == nil {
		if p, ok := ps[targetID]; ok {
			return strings.TrimSpace(p.SKU + " " + p.Name)
		}
	}
	return targetID
}

// applyScenarioChange stores the new line set and a history snapshot so it can be undone.
func (s *Service) applyScenarioChange(ctx context.Context, id, action, detail string, before, after []domain.ScenarioLine) error {
	if err := s.scenarios.ReplaceLines(ctx, id, after); err != nil {
		return err
	}
	return s.scenarios.PushHistory(ctx, id, domain.ScenarioHistoryEntry{Action: action, Detail: detail, Before: before, After: after})
}

// SetScenarioLine adds a product/kit to the scenario or changes its simulated weekly qty.
// weeklyQty nil makes the line follow the real average again.
func (s *Service) SetScenarioLine(ctx context.Context, id, kind, targetID string, weeklyQty *float64) (domain.ScenarioResult, error) {
	if !validScenarioKind(kind) || targetID == "" || (weeklyQty != nil && *weeklyQty < 0) {
		return domain.ScenarioResult{}, domain.ErrInvalid
	}
	if _, err := s.scenarios.Get(ctx, id); err != nil {
		return domain.ScenarioResult{}, err
	}
	if kind == domain.ForecastKindProduct {
		assemblies, err := s.assemblyLookup(ctx)
		if err != nil {
			return domain.ScenarioResult{}, err
		}
		if _, isKit := kitByProductID(assemblies)[targetID]; isKit {
			return domain.ScenarioResult{}, domain.ErrInvalid
		}
	}
	before, err := s.scenarios.Lines(ctx, id)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	after := copyLines(before)
	found := false
	for i := range after {
		if after[i].Kind == kind && after[i].TargetID == targetID {
			after[i].WeeklyQty = weeklyQty
			found = true
		}
	}
	if !found {
		after = append(after, domain.ScenarioLine{Kind: kind, TargetID: targetID, WeeklyQty: weeklyQty})
	}
	if err := s.applyScenarioChange(ctx, id, domain.ScenarioActionSet, s.targetLabel(ctx, kind, targetID), before, after); err != nil {
		return domain.ScenarioResult{}, err
	}
	return s.ScenarioResult(ctx, id)
}

func (s *Service) RemoveScenarioLine(ctx context.Context, id, kind, targetID string) (domain.ScenarioResult, error) {
	if _, err := s.scenarios.Get(ctx, id); err != nil {
		return domain.ScenarioResult{}, err
	}
	before, err := s.scenarios.Lines(ctx, id)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	after := make([]domain.ScenarioLine, 0, len(before))
	for _, l := range before {
		if l.Kind == kind && l.TargetID == targetID {
			continue
		}
		after = append(after, l)
	}
	if len(after) == len(before) {
		return domain.ScenarioResult{}, domain.ErrNotFound
	}
	if err := s.applyScenarioChange(ctx, id, domain.ScenarioActionRemove, s.targetLabel(ctx, kind, targetID), before, copyLines(after)); err != nil {
		return domain.ScenarioResult{}, err
	}
	return s.ScenarioResult(ctx, id)
}

// ResetScenarioToReal makes every line follow the real average again.
func (s *Service) ResetScenarioToReal(ctx context.Context, id string) (domain.ScenarioResult, error) {
	if _, err := s.scenarios.Get(ctx, id); err != nil {
		return domain.ScenarioResult{}, err
	}
	before, err := s.scenarios.Lines(ctx, id)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	after := copyLines(before)
	for i := range after {
		after[i].WeeklyQty = nil
	}
	if err := s.applyScenarioChange(ctx, id, domain.ScenarioActionReset, "", before, after); err != nil {
		return domain.ScenarioResult{}, err
	}
	return s.ScenarioResult(ctx, id)
}

// ImportRealLines adds every product/kit that has real sales in the lookback window and isn't
// in the scenario yet, following the real average (no simulated value).
func (s *Service) ImportRealLines(ctx context.Context, id string) (domain.ScenarioResult, error) {
	sc, err := s.scenarios.Get(ctx, id)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	calc, err := s.productForecastCalc(ctx, sc.LookbackWeeks)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	assemblies, err := s.assemblyLookup(ctx)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	kitProducts := kitByProductID(assemblies)
	before, err := s.scenarios.Lines(ctx, id)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	have := map[[2]string]bool{}
	for _, l := range before {
		have[[2]string{l.Kind, l.TargetID}] = true
	}
	after := copyLines(before)
	var added []domain.ScenarioLine
	for pid, c := range calc {
		if c.computed <= 0 {
			continue
		}
		if _, isKit := kitProducts[pid]; isKit {
			continue
		}
		if !have[[2]string{domain.ForecastKindProduct, pid}] {
			added = append(added, domain.ScenarioLine{Kind: domain.ForecastKindProduct, TargetID: pid})
		}
	}
	for _, a := range assemblies {
		if a.ProductID == "" || calc[a.ProductID].computed <= 0 {
			continue
		}
		if !have[[2]string{domain.ForecastKindKit, a.ID}] {
			added = append(added, domain.ScenarioLine{Kind: domain.ForecastKindKit, TargetID: a.ID})
		}
	}
	sort.Slice(added, func(i, j int) bool {
		if added[i].Kind != added[j].Kind {
			return added[i].Kind < added[j].Kind
		}
		return added[i].TargetID < added[j].TargetID
	})
	if len(added) == 0 {
		return s.ScenarioResult(ctx, id)
	}
	after = append(after, added...)
	if err := s.applyScenarioChange(ctx, id, domain.ScenarioActionImportReal, "", before, after); err != nil {
		return domain.ScenarioResult{}, err
	}
	return s.ScenarioResult(ctx, id)
}

func (s *Service) UndoScenario(ctx context.Context, id string) (domain.ScenarioResult, error) {
	if _, err := s.scenarios.Get(ctx, id); err != nil {
		return domain.ScenarioResult{}, err
	}
	e, err := s.scenarios.LastApplied(ctx, id)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	if e == nil {
		return domain.ScenarioResult{}, domain.ErrInvalid
	}
	if err := s.scenarios.ReplaceLines(ctx, id, e.Before); err != nil {
		return domain.ScenarioResult{}, err
	}
	if err := s.scenarios.SetHistoryUndone(ctx, e.ID, true); err != nil {
		return domain.ScenarioResult{}, err
	}
	return s.ScenarioResult(ctx, id)
}

func (s *Service) RedoScenario(ctx context.Context, id string) (domain.ScenarioResult, error) {
	if _, err := s.scenarios.Get(ctx, id); err != nil {
		return domain.ScenarioResult{}, err
	}
	e, err := s.scenarios.NextUndone(ctx, id)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	if e == nil {
		return domain.ScenarioResult{}, domain.ErrInvalid
	}
	if err := s.scenarios.ReplaceLines(ctx, id, e.After); err != nil {
		return domain.ScenarioResult{}, err
	}
	if err := s.scenarios.SetHistoryUndone(ctx, e.ID, false); err != nil {
		return domain.ScenarioResult{}, err
	}
	return s.ScenarioResult(ctx, id)
}

// ScenarioResult evaluates a scenario against current real data: per-line real vs simulated
// weekly quantity and revenue/cost/profit, the stock projection after expanding kits into
// components (what to keep and what to buy), and the totals.
func (s *Service) ScenarioResult(ctx context.Context, id string) (domain.ScenarioResult, error) {
	sc, err := s.scenarios.Get(ctx, id)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	lines, err := s.scenarios.Lines(ctx, id)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	calc, err := s.productForecastCalc(ctx, sc.LookbackWeeks)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	products, err := s.productLookup(ctx)
	if err != nil {
		return domain.ScenarioResult{}, err
	}
	assemblies, err := s.assemblyLookup(ctx)
	if err != nil {
		return domain.ScenarioResult{}, err
	}

	res := domain.ScenarioResult{Scenario: sc, Lines: make([]domain.ScenarioLineResult, 0, len(lines)), Stock: []domain.ScenarioStockLine{}}
	demand := map[string]float64{}
	for _, l := range lines {
		lr := domain.ScenarioLineResult{Kind: l.Kind, TargetID: l.TargetID, SimWeeklyQty: l.WeeklyQty}
		if l.Kind == domain.ForecastKindKit {
			a, ok := assemblies[l.TargetID]
			if !ok {
				lr.Name = "(kit removido)"
				res.Lines = append(res.Lines, lr)
				continue
			}
			lr.Code, lr.Name, lr.UoM = a.Code, a.Name, "un"
			if a.ProductID != "" {
				lr.RealWeeklyQty = calc[a.ProductID].computed
				lr.UnitRevenue = products[a.ProductID].SalePrice
			}
			lr.UnitCost = a.Cost
		} else {
			p, ok := products[l.TargetID]
			if !ok {
				lr.Name = "(produto removido)"
				res.Lines = append(res.Lines, lr)
				continue
			}
			lr.Code, lr.Name, lr.UoM = p.SKU, p.Name, p.StockUnitOfMeasure()
			lr.RealWeeklyQty = calc[l.TargetID].computed
			lr.UnitRevenue = p.SalePrice
			lr.UnitCost = p.PurchasePrice
		}
		lr.EffectiveWeeklyQty = lr.RealWeeklyQty
		if l.WeeklyQty != nil {
			lr.EffectiveWeeklyQty = *l.WeeklyQty
		}
		lr.DeltaQty = lr.EffectiveWeeklyQty - lr.RealWeeklyQty
		if lr.RealWeeklyQty > 0 {
			d := lr.DeltaQty / lr.RealWeeklyQty * 100
			lr.DeltaPercent = &d
		}
		lr.WeeklyRevenue = lr.EffectiveWeeklyQty * lr.UnitRevenue
		lr.WeeklyCost = lr.EffectiveWeeklyQty * lr.UnitCost
		lr.WeeklyProfit = lr.WeeklyRevenue - lr.WeeklyCost
		if lr.WeeklyRevenue > 0 {
			lr.MarginPercent = lr.WeeklyProfit / lr.WeeklyRevenue * 100
		}
		res.Lines = append(res.Lines, lr)
		res.Summary.WeeklyRevenue += lr.WeeklyRevenue
		res.Summary.WeeklyCost += lr.WeeklyCost

		if l.Kind == domain.ForecastKindKit {
			for _, it := range assemblies[l.TargetID].Items {
				demand[it.ProductID] += lr.EffectiveWeeklyQty * it.Quantity
			}
		} else {
			demand[l.TargetID] += lr.EffectiveWeeklyQty
		}
	}
	res.Summary.WeeklyProfit = res.Summary.WeeklyRevenue - res.Summary.WeeklyCost
	if res.Summary.WeeklyRevenue > 0 {
		res.Summary.MarginPercent = res.Summary.WeeklyProfit / res.Summary.WeeklyRevenue * 100
	}
	res.Summary.CoverageRevenue = res.Summary.WeeklyRevenue * float64(sc.CoverageWeeks)

	if len(demand) > 0 {
		balances, err := s.catalog.Balances(ctx)
		if err != nil {
			return domain.ScenarioResult{}, err
		}
		onHand := map[string]float64{}
		for _, b := range balances {
			onHand[b.ProductID] += b.QuantityAvailable
		}
		openPO, err := s.purchasing.OpenOrderQuantities(ctx)
		if err != nil {
			return domain.ScenarioResult{}, err
		}
		prices, err := s.supplierPrices.List(ctx)
		if err != nil {
			return domain.ScenarioResult{}, err
		}
		for pid, qty := range demand {
			p := products[pid]
			_, supplierUnit, hasSupplier := bestSupplierUnitPrice(p, prices)
			unitCost := domain.StockUnitCost(p, supplierUnit, hasSupplier)
			target := math.Ceil(qty*float64(sc.CoverageWeeks)*(1+sc.SafetyPercent/100) - 1e-9)
			if target < 0 {
				target = 0
			}
			toBuy := domain.NeededQty(qty, sc.CoverageWeeks, sc.SafetyPercent, onHand[pid], openPO[pid])
			sl := domain.ScenarioStockLine{
				ProductID: pid, SKU: p.SKU, Name: p.Name, UoM: p.StockUnitOfMeasure(),
				WeeklyDemand: qty, TargetStock: target, OnHandQty: onHand[pid], OpenPOQty: openPO[pid],
				ToBuyQty: toBuy, UnitCost: unitCost, PurchaseCost: toBuy * unitCost, StockValue: target * unitCost,
			}
			res.Stock = append(res.Stock, sl)
			res.Summary.PurchaseCost += sl.PurchaseCost
			res.Summary.StockValue += sl.StockValue
		}
		sort.Slice(res.Stock, func(i, j int) bool { return res.Stock[i].SKU < res.Stock[j].SKU })
	}

	if e, err := s.scenarios.LastApplied(ctx, id); err == nil && e != nil {
		res.CanUndo, res.UndoAction, res.UndoDetail = true, e.Action, e.Detail
	}
	if e, err := s.scenarios.NextUndone(ctx, id); err == nil && e != nil {
		res.CanRedo, res.RedoAction, res.RedoDetail = true, e.Action, e.Detail
	}
	return res, nil
}
