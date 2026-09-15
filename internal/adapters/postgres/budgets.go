package postgres

import (
	"context"
	"errors"

	"erp/services/bi-service/internal/domain"

	"github.com/jackc/pgx/v5"
)

type Budgets struct{ *Repo }

func (r Budgets) Create(ctx context.Context, b domain.Budget) (domain.Budget, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Budget{}, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `
		INSERT INTO budgets (code, status, coverage_weeks, safety_percent, lookback_weeks, schedule_id)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at, updated_at
	`, b.Code, b.Status, b.CoverageWeeks, b.SafetyPercent, b.LookbackWeeks, b.ScheduleID).Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return domain.Budget{}, err
	}
	for i, item := range b.Items {
		if err := tx.QueryRow(ctx, `
			INSERT INTO budget_items (budget_id, product_id, forecast_qty, on_hand_qty, open_po_qty, needed_qty)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id
		`, b.ID, item.ProductID, item.ForecastQty, item.OnHandQty, item.OpenPOQty, item.NeededQty).Scan(&b.Items[i].ID); err != nil {
			return domain.Budget{}, err
		}
		b.Items[i].BudgetID = b.ID
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Budget{}, err
	}
	return b, nil
}

func (r Budgets) Get(ctx context.Context, id string) (domain.Budget, error) {
	var b domain.Budget
	err := r.pool.QueryRow(ctx, `
		SELECT id, code, status, coverage_weeks, safety_percent, lookback_weeks, schedule_id, created_at, updated_at
		FROM budgets WHERE id=$1
	`, id).Scan(&b.ID, &b.Code, &b.Status, &b.CoverageWeeks, &b.SafetyPercent, &b.LookbackWeeks, &b.ScheduleID, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Budget{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Budget{}, err
	}
	items, err := r.items(ctx, id)
	if err != nil {
		return domain.Budget{}, err
	}
	b.Items = items
	return b, nil
}

func (r Budgets) List(ctx context.Context) ([]domain.Budget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, status, coverage_weeks, safety_percent, lookback_weeks, schedule_id, created_at, updated_at
		FROM budgets ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Budget
	for rows.Next() {
		var b domain.Budget
		if err := rows.Scan(&b.ID, &b.Code, &b.Status, &b.CoverageWeeks, &b.SafetyPercent, &b.LookbackWeeks, &b.ScheduleID, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		items, err := r.items(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Items = items
	}
	if out == nil {
		out = []domain.Budget{}
	}
	return out, nil
}

func (r Budgets) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM budgets WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r Budgets) UpdateStatus(ctx context.Context, id, status string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE budgets SET status=$2, updated_at=NOW() WHERE id=$1`, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r Budgets) GetItem(ctx context.Context, budgetID, itemID string) (domain.BudgetItem, error) {
	var it domain.BudgetItem
	err := r.pool.QueryRow(ctx, `
		SELECT id, budget_id, product_id, forecast_qty, on_hand_qty, open_po_qty, needed_qty
		FROM budget_items WHERE id=$1 AND budget_id=$2
	`, itemID, budgetID).Scan(&it.ID, &it.BudgetID, &it.ProductID, &it.ForecastQty, &it.OnHandQty, &it.OpenPOQty, &it.NeededQty)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BudgetItem{}, domain.ErrNotFound
	}
	return it, err
}

func (r Budgets) UpdateItemNeededQty(ctx context.Context, itemID string, neededQty float64) (domain.BudgetItem, error) {
	var it domain.BudgetItem
	err := r.pool.QueryRow(ctx, `
		UPDATE budget_items SET needed_qty=$2 WHERE id=$1
		RETURNING id, budget_id, product_id, forecast_qty, on_hand_qty, open_po_qty, needed_qty
	`, itemID, neededQty).Scan(&it.ID, &it.BudgetID, &it.ProductID, &it.ForecastQty, &it.OnHandQty, &it.OpenPOQty, &it.NeededQty)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BudgetItem{}, domain.ErrNotFound
	}
	return it, err
}

func (r Budgets) DeleteItem(ctx context.Context, budgetID, itemID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM budget_items WHERE id=$1 AND budget_id=$2`, itemID, budgetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r Budgets) ItemAllocatedQty(ctx context.Context, itemID string) (float64, error) {
	var total float64
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(quantity),0) FROM budget_allocations WHERE budget_item_id=$1`, itemID).Scan(&total)
	return total, err
}

func (r Budgets) AddAllocation(ctx context.Context, a domain.BudgetAllocation) (domain.BudgetAllocation, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO budget_allocations (budget_item_id, supplier_id, quantity, unit_price)
		VALUES ($1,$2,$3,$4) RETURNING id, created_at
	`, a.BudgetItemID, a.SupplierID, a.Quantity, a.UnitPrice).Scan(&a.ID, &a.CreatedAt)
	return a, err
}

func (r Budgets) DeleteAllocation(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM budget_allocations WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r Budgets) SetAllocationQuote(ctx context.Context, id, quoteID string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE budget_allocations SET quote_id=$2 WHERE id=$1`, id, quoteID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r Budgets) items(ctx context.Context, budgetID string) ([]domain.BudgetItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, budget_id, product_id, forecast_qty, on_hand_qty, open_po_qty, needed_qty
		FROM budget_items WHERE budget_id=$1 ORDER BY product_id
	`, budgetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BudgetItem
	var ids []string
	for rows.Next() {
		var it domain.BudgetItem
		if err := rows.Scan(&it.ID, &it.BudgetID, &it.ProductID, &it.ForecastQty, &it.OnHandQty, &it.OpenPOQty, &it.NeededQty); err != nil {
			return nil, err
		}
		out = append(out, it)
		ids = append(ids, it.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	allocByItem, err := r.allocationsByItem(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Allocations = allocByItem[out[i].ID]
	}
	if out == nil {
		out = []domain.BudgetItem{}
	}
	return out, nil
}

func (r Budgets) allocationsByItem(ctx context.Context, itemIDs []string) (map[string][]domain.BudgetAllocation, error) {
	out := map[string][]domain.BudgetAllocation{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, budget_item_id, supplier_id, quantity, unit_price, quote_id, created_at
		FROM budget_allocations WHERE budget_item_id = ANY($1::uuid[]) ORDER BY created_at
	`, itemIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a domain.BudgetAllocation
		if err := rows.Scan(&a.ID, &a.BudgetItemID, &a.SupplierID, &a.Quantity, &a.UnitPrice, &a.QuoteID, &a.CreatedAt); err != nil {
			return nil, err
		}
		out[a.BudgetItemID] = append(out[a.BudgetItemID], a)
	}
	return out, rows.Err()
}
