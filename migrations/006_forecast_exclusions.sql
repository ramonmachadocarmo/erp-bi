CREATE TABLE forecast_exclusions (
    kind VARCHAR(10) NOT NULL,
    target_id VARCHAR(50) NOT NULL,
    excluded_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (kind, target_id)
);
