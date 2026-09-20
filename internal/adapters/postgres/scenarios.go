package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"erp/services/bi-service/internal/domain"

	"github.com/jackc/pgx/v5"
)

type Scenarios struct{ *Repo }

const scenarioCols = `id, name, notes, coverage_weeks, safety_percent, lookback_weeks, created_at, updated_at`

func scanScenario(row interface {
	Scan(dest ...any) error
}, withCount bool) (domain.Scenario, error) {
	var s domain.Scenario
	var err error
	if withCount {
		err = row.Scan(&s.ID, &s.Name, &s.Notes, &s.CoverageWeeks, &s.SafetyPercent, &s.LookbackWeeks, &s.CreatedAt, &s.UpdatedAt, &s.LineCount)
	} else {
		err = row.Scan(&s.ID, &s.Name, &s.Notes, &s.CoverageWeeks, &s.SafetyPercent, &s.LookbackWeeks, &s.CreatedAt, &s.UpdatedAt)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Scenario{}, domain.ErrNotFound
	}
	return s, err
}

func (r Scenarios) Create(ctx context.Context, s domain.Scenario) (domain.Scenario, error) {
	return scanScenario(r.pool.QueryRow(ctx, `
		INSERT INTO bi_scenarios (name, notes, coverage_weeks, safety_percent, lookback_weeks)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING `+scenarioCols, s.Name, s.Notes, s.CoverageWeeks, s.SafetyPercent, s.LookbackWeeks), false)
}

func (r Scenarios) Get(ctx context.Context, id string) (domain.Scenario, error) {
	s, err := scanScenario(r.pool.QueryRow(ctx, `
		SELECT `+scenarioCols+`, (SELECT COUNT(1) FROM bi_scenario_lines l WHERE l.scenario_id = bi_scenarios.id)
		FROM bi_scenarios WHERE id=$1`, id), true)
	return s, err
}

func (r Scenarios) List(ctx context.Context) ([]domain.Scenario, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+scenarioCols+`, (SELECT COUNT(1) FROM bi_scenario_lines l WHERE l.scenario_id = bi_scenarios.id)
		FROM bi_scenarios ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Scenario{}
	for rows.Next() {
		s, err := scanScenario(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r Scenarios) Update(ctx context.Context, s domain.Scenario) (domain.Scenario, error) {
	return scanScenario(r.pool.QueryRow(ctx, `
		UPDATE bi_scenarios SET name=$2, notes=$3, coverage_weeks=$4, safety_percent=$5, lookback_weeks=$6, updated_at=NOW()
		WHERE id=$1
		RETURNING `+scenarioCols, s.ID, s.Name, s.Notes, s.CoverageWeeks, s.SafetyPercent, s.LookbackWeeks), false)
}

func (r Scenarios) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM bi_scenarios WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r Scenarios) Lines(ctx context.Context, id string) ([]domain.ScenarioLine, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT kind, target_id, weekly_qty FROM bi_scenario_lines WHERE scenario_id=$1 ORDER BY position, kind, target_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ScenarioLine{}
	for rows.Next() {
		var l domain.ScenarioLine
		if err := rows.Scan(&l.Kind, &l.TargetID, &l.WeeklyQty); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r Scenarios) ReplaceLines(ctx context.Context, id string, lines []domain.ScenarioLine) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM bi_scenario_lines WHERE scenario_id=$1`, id); err != nil {
		return err
	}
	for i, l := range lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO bi_scenario_lines (scenario_id, kind, target_id, weekly_qty, position) VALUES ($1,$2,$3,$4,$5)`,
			id, l.Kind, l.TargetID, l.WeeklyQty, i); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE bi_scenarios SET updated_at=NOW() WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r Scenarios) PushHistory(ctx context.Context, id string, e domain.ScenarioHistoryEntry) error {
	before, err := json.Marshal(e.Before)
	if err != nil {
		return err
	}
	after, err := json.Marshal(e.After)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM bi_scenario_history WHERE scenario_id=$1 AND undone`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO bi_scenario_history (scenario_id, action, detail, before_lines, after_lines) VALUES ($1,$2,$3,$4,$5)`,
		id, e.Action, e.Detail, before, after); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM bi_scenario_history WHERE scenario_id=$1 AND id NOT IN (
			SELECT id FROM bi_scenario_history WHERE scenario_id=$1 ORDER BY seq DESC LIMIT $2)`,
		id, domain.ScenarioHistoryLimit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r Scenarios) historyEntry(ctx context.Context, query, id string) (*domain.ScenarioHistoryEntry, error) {
	var e domain.ScenarioHistoryEntry
	var before, after []byte
	err := r.pool.QueryRow(ctx, query, id).Scan(&e.ID, &e.Action, &e.Detail, &before, &after, &e.Undone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(before, &e.Before); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(after, &e.After); err != nil {
		return nil, err
	}
	return &e, nil
}

func (r Scenarios) LastApplied(ctx context.Context, id string) (*domain.ScenarioHistoryEntry, error) {
	return r.historyEntry(ctx, `
		SELECT id, action, detail, before_lines, after_lines, undone FROM bi_scenario_history
		WHERE scenario_id=$1 AND NOT undone ORDER BY seq DESC LIMIT 1`, id)
}

func (r Scenarios) NextUndone(ctx context.Context, id string) (*domain.ScenarioHistoryEntry, error) {
	return r.historyEntry(ctx, `
		SELECT id, action, detail, before_lines, after_lines, undone FROM bi_scenario_history
		WHERE scenario_id=$1 AND undone ORDER BY seq ASC LIMIT 1`, id)
}

func (r Scenarios) SetHistoryUndone(ctx context.Context, entryID string, undone bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE bi_scenario_history SET undone=$2 WHERE id=$1`, entryID, undone)
	return err
}
