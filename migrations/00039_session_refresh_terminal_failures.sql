-- +goose Up
ALTER TABLE session_refresh_jobs
  DROP CONSTRAINT session_refresh_jobs_status_check,
  ADD CONSTRAINT session_refresh_jobs_status_check
    CHECK(status IN ('pending','leased','succeeded','failed','cancelled'));

DROP INDEX accounts_session_refresh_due_idx;
CREATE INDEX accounts_session_refresh_due_idx
  ON accounts(access_token_expires_at,id)
  WHERE session_refresh_enabled=true AND status NOT IN ('disabled','invalid');

-- +goose Down
UPDATE session_refresh_jobs
SET status='cancelled'
WHERE status='failed';

ALTER TABLE session_refresh_jobs
  DROP CONSTRAINT session_refresh_jobs_status_check,
  ADD CONSTRAINT session_refresh_jobs_status_check
    CHECK(status IN ('pending','leased','succeeded','cancelled'));

DROP INDEX accounts_session_refresh_due_idx;
CREATE INDEX accounts_session_refresh_due_idx
  ON accounts(access_token_expires_at,id)
  WHERE session_refresh_enabled=true AND status<>'disabled';
