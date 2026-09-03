-- +goose Up
ALTER TABLE accounts
  ADD COLUMN generation_permission_status text NOT NULL DEFAULT 'unknown',
  ADD COLUMN generation_permission_checked_at timestamptz,
  ADD COLUMN generation_permission_model text NOT NULL DEFAULT '',
  ADD COLUMN generation_permission_error_code text NOT NULL DEFAULT '',
  ADD COLUMN generation_permission_error text NOT NULL DEFAULT '';

ALTER TABLE accounts
  ADD CONSTRAINT accounts_generation_permission_status_check
    CHECK (generation_permission_status IN ('unknown','verified','blocked','rate_limited','error'));

CREATE INDEX accounts_generation_permission_due_idx
  ON accounts(generation_permission_checked_at,id)
  WHERE archived_at IS NULL AND status <> 'disabled';

-- Existing accounts are intentionally unknown until the first session refresh
-- runs the provider-specific generation permission check.

-- +goose Down
DROP INDEX IF EXISTS accounts_generation_permission_due_idx;
ALTER TABLE accounts
  DROP CONSTRAINT IF EXISTS accounts_generation_permission_status_check,
  DROP COLUMN IF EXISTS generation_permission_error,
  DROP COLUMN IF EXISTS generation_permission_error_code,
  DROP COLUMN IF EXISTS generation_permission_model,
  DROP COLUMN IF EXISTS generation_permission_checked_at,
  DROP COLUMN IF EXISTS generation_permission_status;
