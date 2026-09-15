ALTER TABLE forecast_overrides RENAME COLUMN product_id TO target_id;
ALTER TABLE forecast_overrides ADD COLUMN kind VARCHAR(10) NOT NULL DEFAULT 'PRODUCT';
ALTER TABLE forecast_overrides DROP CONSTRAINT forecast_overrides_pkey;
ALTER TABLE forecast_overrides ADD PRIMARY KEY (kind, target_id);
