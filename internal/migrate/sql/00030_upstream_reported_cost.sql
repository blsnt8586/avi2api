-- +goose Up
ALTER TABLE tasks
  ADD COLUMN upstream_reported_cost double precision
  CHECK (upstream_reported_cost IS NULL OR upstream_reported_cost >= 0);

-- +goose Down
ALTER TABLE tasks DROP COLUMN IF EXISTS upstream_reported_cost;
