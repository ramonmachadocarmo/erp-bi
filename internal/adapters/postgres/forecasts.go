package postgres

import (
	"context"

	"erp/services/bi-service/internal/domain"
)

type Forecasts struct{ *Repo }

func (f Forecasts) ListOverrides(ctx context.Context) ([]domain.ForecastOverride, error) {
	rows, err := f.pool.Query(ctx, `
		SELECT kind, target_id, weekly_qty, note, updated_by, updated_at FROM forecast_overrides
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ForecastOverride
	for rows.Next() {
		var o domain.ForecastOverride
		if err := rows.Scan(&o.Kind, &o.TargetID, &o.WeeklyQty, &o.Note, &o.UpdatedBy, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if out == nil {
		out = []domain.ForecastOverride{}
	}
	return out, rows.Err()
}

func (f Forecasts) SetOverride(ctx context.Context, o domain.ForecastOverride) (domain.ForecastOverride, error) {
	err := f.pool.QueryRow(ctx, `
		INSERT INTO forecast_overrides (kind, target_id, weekly_qty, note, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,NOW())
		ON CONFLICT (kind, target_id) DO UPDATE SET weekly_qty=$3, note=$4, updated_by=$5, updated_at=NOW()
		RETURNING kind, target_id, weekly_qty, note, updated_by, updated_at
	`, o.Kind, o.TargetID, o.WeeklyQty, o.Note, o.UpdatedBy).Scan(&o.Kind, &o.TargetID, &o.WeeklyQty, &o.Note, &o.UpdatedBy, &o.UpdatedAt)
	if err != nil {
		return domain.ForecastOverride{}, err
	}
	return o, nil
}

func (f Forecasts) DeleteOverride(ctx context.Context, kind, targetID string) error {
	tag, err := f.pool.Exec(ctx, `DELETE FROM forecast_overrides WHERE kind=$1 AND target_id=$2`, kind, targetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (f Forecasts) ListExclusions(ctx context.Context) ([]domain.ForecastExclusion, error) {
	rows, err := f.pool.Query(ctx, `SELECT kind, target_id, excluded_at FROM forecast_exclusions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ForecastExclusion
	for rows.Next() {
		var e domain.ForecastExclusion
		if err := rows.Scan(&e.Kind, &e.TargetID, &e.ExcludedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if out == nil {
		out = []domain.ForecastExclusion{}
	}
	return out, rows.Err()
}

func (f Forecasts) SetExclusion(ctx context.Context, kind, targetID string) error {
	_, err := f.pool.Exec(ctx, `
		INSERT INTO forecast_exclusions (kind, target_id) VALUES ($1,$2)
		ON CONFLICT (kind, target_id) DO NOTHING
	`, kind, targetID)
	return err
}

func (f Forecasts) DeleteExclusion(ctx context.Context, kind, targetID string) error {
	tag, err := f.pool.Exec(ctx, `DELETE FROM forecast_exclusions WHERE kind=$1 AND target_id=$2`, kind, targetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
