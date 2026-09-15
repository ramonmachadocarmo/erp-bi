package postgres

import (
	"context"

	"erp/services/bi-service/internal/domain"
)

type SupplierPrices struct{ *Repo }

func (r SupplierPrices) List(ctx context.Context) ([]domain.SupplierPrice, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, product_id, supplier_id, price, min_qty, updated_at FROM supplier_prices ORDER BY product_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SupplierPrice
	for rows.Next() {
		var sp domain.SupplierPrice
		if err := rows.Scan(&sp.ID, &sp.ProductID, &sp.SupplierID, &sp.Price, &sp.MinQty, &sp.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	if out == nil {
		out = []domain.SupplierPrice{}
	}
	return out, rows.Err()
}

func (r SupplierPrices) Upsert(ctx context.Context, sp domain.SupplierPrice) (domain.SupplierPrice, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO supplier_prices (product_id, supplier_id, price, min_qty, updated_at)
		VALUES ($1,$2,$3,$4,NOW())
		ON CONFLICT (product_id, supplier_id) DO UPDATE SET price=$3, min_qty=$4, updated_at=NOW()
		RETURNING id, product_id, supplier_id, price, min_qty, updated_at
	`, sp.ProductID, sp.SupplierID, sp.Price, sp.MinQty).Scan(&sp.ID, &sp.ProductID, &sp.SupplierID, &sp.Price, &sp.MinQty, &sp.UpdatedAt)
	if err != nil {
		return domain.SupplierPrice{}, err
	}
	return sp, nil
}

func (r SupplierPrices) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM supplier_prices WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
