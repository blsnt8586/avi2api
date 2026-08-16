-- +goose Up
ALTER TABLE accounts
  ADD COLUMN archived_at timestamptz;

CREATE INDEX accounts_active_pool_idx
  ON accounts(provider_id,status,created_at,id)
  WHERE archived_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS accounts_active_pool_idx;
ALTER TABLE accounts DROP COLUMN archived_at;
