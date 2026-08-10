-- +goose Up
ALTER TABLE api_keys
  ADD COLUMN description text NOT NULL DEFAULT '',
  ADD COLUMN expires_at timestamptz;

-- +goose Down
ALTER TABLE api_keys
  DROP COLUMN expires_at,
  DROP COLUMN description;
