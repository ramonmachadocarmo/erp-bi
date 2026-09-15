CREATE TABLE forecast_overrides (
    product_id VARCHAR(50) PRIMARY KEY,
    weekly_qty NUMERIC(15,4) NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    updated_by VARCHAR(255) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE budget_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    frequency VARCHAR(20) NOT NULL,
    day_of_week SMALLINT,
    day_of_month SMALLINT,
    coverage_weeks INT NOT NULL,
    safety_percent NUMERIC(6,2) NOT NULL DEFAULT 0,
    lookback_weeks INT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at TIMESTAMPTZ NOT NULL,
    last_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE budgets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(20) NOT NULL UNIQUE,
    status VARCHAR(20) NOT NULL,
    coverage_weeks INT NOT NULL,
    safety_percent NUMERIC(6,2) NOT NULL DEFAULT 0,
    lookback_weeks INT NOT NULL,
    schedule_id UUID REFERENCES budget_schedules(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE budget_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    budget_id UUID NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
    product_id VARCHAR(50) NOT NULL,
    forecast_qty NUMERIC(15,4) NOT NULL DEFAULT 0,
    on_hand_qty NUMERIC(15,4) NOT NULL DEFAULT 0,
    open_po_qty NUMERIC(15,4) NOT NULL DEFAULT 0,
    needed_qty NUMERIC(15,4) NOT NULL DEFAULT 0,
    UNIQUE (budget_id, product_id)
);

CREATE TABLE budget_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    budget_item_id UUID NOT NULL REFERENCES budget_items(id) ON DELETE CASCADE,
    supplier_id UUID NOT NULL,
    quantity NUMERIC(15,4) NOT NULL CHECK (quantity > 0),
    unit_price NUMERIC(15,4) NOT NULL DEFAULT 0,
    quote_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_budget_items_budget ON budget_items(budget_id);
CREATE INDEX idx_budget_allocations_item ON budget_allocations(budget_item_id);
