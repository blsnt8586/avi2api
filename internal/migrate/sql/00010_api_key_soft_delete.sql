-- +goose Up
ALTER TABLE api_keys ADD COLUMN deleted_at timestamptz;
CREATE INDEX api_keys_active_created_idx ON api_keys(created_at DESC) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS api_keys_active_created_idx;
ALTER TABLE api_keys DROP COLUMN deleted_at;
