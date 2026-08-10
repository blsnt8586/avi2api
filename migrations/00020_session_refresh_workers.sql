-- +goose Up
ALTER TABLE accounts
  ADD COLUMN session_refresh_enabled boolean NOT NULL DEFAULT true,
  ADD COLUMN browser_profile_key text NOT NULL DEFAULT '',
  ADD COLUMN session_refresh_not_before timestamptz,
  ADD COLUMN session_refresh_last_at timestamptz,
  ADD COLUMN session_refresh_last_method text NOT NULL DEFAULT '',
  ADD COLUMN session_refresh_last_duration_ms integer,
  ADD COLUMN session_refresh_failures integer NOT NULL DEFAULT 0;

UPDATE accounts SET browser_profile_key=id::text WHERE browser_profile_key='';

ALTER TABLE accounts
  ADD CONSTRAINT accounts_session_refresh_duration_check
    CHECK(session_refresh_last_duration_ms IS NULL OR session_refresh_last_duration_ms>=0),
  ADD CONSTRAINT accounts_session_refresh_failures_check
    CHECK(session_refresh_failures>=0),
  ADD CONSTRAINT accounts_session_refresh_method_check
    CHECK(session_refresh_last_method IN ('','cookie','browser'));

CREATE TABLE session_refresh_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  stage text NOT NULL DEFAULT 'cookie' CHECK(stage IN ('cookie','browser')),
  status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','leased','succeeded','cancelled')),
  priority integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  attempt_count integer NOT NULL DEFAULT 0 CHECK(attempt_count>=0),
  lease_owner text NOT NULL DEFAULT '',
  lease_token uuid,
  lease_started_at timestamptz,
  lease_expires_at timestamptz,
  last_error text NOT NULL DEFAULT '',
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX session_refresh_jobs_active_account_idx
  ON session_refresh_jobs(account_id)
  WHERE status IN ('pending','leased');
CREATE INDEX session_refresh_jobs_claim_idx
  ON session_refresh_jobs(stage,next_attempt_at,priority DESC,created_at)
  WHERE status='pending';
CREATE INDEX session_refresh_jobs_expired_lease_idx
  ON session_refresh_jobs(lease_expires_at)
  WHERE status='leased';
CREATE INDEX session_refresh_jobs_account_history_idx
  ON session_refresh_jobs(account_id,created_at DESC);
CREATE INDEX accounts_session_refresh_due_idx
  ON accounts(access_token_expires_at,id)
  WHERE session_refresh_enabled=true AND status<>'disabled';

-- +goose Down
DROP TABLE IF EXISTS session_refresh_jobs;
DROP INDEX IF EXISTS accounts_session_refresh_due_idx;
ALTER TABLE accounts
  DROP CONSTRAINT IF EXISTS accounts_session_refresh_method_check,
  DROP CONSTRAINT IF EXISTS accounts_session_refresh_failures_check,
  DROP CONSTRAINT IF EXISTS accounts_session_refresh_duration_check,
  DROP COLUMN IF EXISTS session_refresh_failures,
  DROP COLUMN IF EXISTS session_refresh_last_duration_ms,
  DROP COLUMN IF EXISTS session_refresh_last_method,
  DROP COLUMN IF EXISTS session_refresh_last_at,
  DROP COLUMN IF EXISTS session_refresh_not_before,
  DROP COLUMN IF EXISTS browser_profile_key,
  DROP COLUMN IF EXISTS session_refresh_enabled;
