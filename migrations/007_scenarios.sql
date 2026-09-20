CREATE TABLE bi_scenarios (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    coverage_weeks INT NOT NULL DEFAULT 4,
    safety_percent NUMERIC(8,2) NOT NULL DEFAULT 10,
    lookback_weeks INT NOT NULL DEFAULT 8,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- weekly_qty NULL means the line follows the real (computed) sales average.
CREATE TABLE bi_scenario_lines (
    scenario_id UUID NOT NULL REFERENCES bi_scenarios(id) ON DELETE CASCADE,
    kind VARCHAR(10) NOT NULL,
    target_id VARCHAR(50) NOT NULL,
    weekly_qty NUMERIC(15,4),
    position INT NOT NULL DEFAULT 0,
    PRIMARY KEY (scenario_id, kind, target_id)
);

-- Full snapshots of the line set before/after each change: undo/redo just restores one.
CREATE TABLE bi_scenario_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scenario_id UUID NOT NULL REFERENCES bi_scenarios(id) ON DELETE CASCADE,
    seq BIGSERIAL,
    action VARCHAR(20) NOT NULL,
    detail VARCHAR(200) NOT NULL DEFAULT '',
    before_lines JSONB NOT NULL,
    after_lines JSONB NOT NULL,
    undone BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_bi_scenario_history_scenario ON bi_scenario_history(scenario_id, seq);
