package postgres

import (
	"context"
	"errors"
	"time"

	"erp/services/bi-service/internal/domain"

	"github.com/jackc/pgx/v5"
)

type Schedules struct{ *Repo }

const scheduleCols = `id, name, frequency, day_of_week, day_of_month, coverage_weeks, safety_percent, lookback_weeks, active, next_run_at, last_run_at, created_at, updated_at`

func scanSchedule(row interface {
	Scan(dest ...any) error
}) (domain.BudgetSchedule, error) {
	var s domain.BudgetSchedule
	err := row.Scan(&s.ID, &s.Name, &s.Frequency, &s.DayOfWeek, &s.DayOfMonth, &s.CoverageWeeks, &s.SafetyPercent, &s.LookbackWeeks, &s.Active, &s.NextRunAt, &s.LastRunAt, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BudgetSchedule{}, domain.ErrNotFound
	}
	return s, err
}

func (r Schedules) Create(ctx context.Context, s domain.BudgetSchedule) (domain.BudgetSchedule, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO budget_schedules (name, frequency, day_of_week, day_of_month, coverage_weeks, safety_percent, lookback_weeks, active, next_run_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+scheduleCols,
		s.Name, s.Frequency, s.DayOfWeek, s.DayOfMonth, s.CoverageWeeks, s.SafetyPercent, s.LookbackWeeks, s.Active, s.NextRunAt,
	).Scan(&s.ID, &s.Name, &s.Frequency, &s.DayOfWeek, &s.DayOfMonth, &s.CoverageWeeks, &s.SafetyPercent, &s.LookbackWeeks, &s.Active, &s.NextRunAt, &s.LastRunAt, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func (r Schedules) Get(ctx context.Context, id string) (domain.BudgetSchedule, error) {
	return scanSchedule(r.pool.QueryRow(ctx, `SELECT `+scheduleCols+` FROM budget_schedules WHERE id=$1`, id))
}

func (r Schedules) List(ctx context.Context) ([]domain.BudgetSchedule, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+scheduleCols+` FROM budget_schedules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BudgetSchedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if out == nil {
		out = []domain.BudgetSchedule{}
	}
	return out, rows.Err()
}

func (r Schedules) Update(ctx context.Context, s domain.BudgetSchedule) (domain.BudgetSchedule, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE budget_schedules SET name=$2, frequency=$3, day_of_week=$4, day_of_month=$5,
			coverage_weeks=$6, safety_percent=$7, lookback_weeks=$8, active=$9, next_run_at=$10, updated_at=NOW()
		WHERE id=$1
	`, s.ID, s.Name, s.Frequency, s.DayOfWeek, s.DayOfMonth, s.CoverageWeeks, s.SafetyPercent, s.LookbackWeeks, s.Active, s.NextRunAt)
	if err != nil {
		return domain.BudgetSchedule{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.BudgetSchedule{}, domain.ErrNotFound
	}
	return r.Get(ctx, s.ID)
}

func (r Schedules) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM budget_schedules WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r Schedules) Due(ctx context.Context, asOf time.Time) ([]domain.BudgetSchedule, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+scheduleCols+` FROM budget_schedules WHERE active AND next_run_at <= $1`, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BudgetSchedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if out == nil {
		out = []domain.BudgetSchedule{}
	}
	return out, rows.Err()
}

func (r Schedules) MarkRun(ctx context.Context, id string, lastRun, nextRun time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE budget_schedules SET last_run_at=$2, next_run_at=$3, updated_at=NOW() WHERE id=$1`, id, lastRun, nextRun)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
