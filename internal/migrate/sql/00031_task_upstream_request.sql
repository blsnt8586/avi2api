-- +goose Up
ALTER TABLE tasks
  ADD COLUMN upstream_request jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE tasks DROP COLUMN IF EXISTS upstream_request;
