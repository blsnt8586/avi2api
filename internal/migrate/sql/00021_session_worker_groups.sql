-- +goose Up
ALTER TABLE accounts
  ADD COLUMN browser_worker_group text NOT NULL DEFAULT 'default';

ALTER TABLE accounts
  ADD CONSTRAINT accounts_browser_worker_group_check
  CHECK(length(browser_worker_group) BETWEEN 1 AND 100);

CREATE INDEX accounts_browser_worker_group_idx
  ON accounts(browser_worker_group,id)
  WHERE session_refresh_enabled=true AND status<>'disabled';

-- +goose Down
DROP INDEX IF EXISTS accounts_browser_worker_group_idx;
ALTER TABLE accounts
  DROP CONSTRAINT IF EXISTS accounts_browser_worker_group_check,
  DROP COLUMN IF EXISTS browser_worker_group;
