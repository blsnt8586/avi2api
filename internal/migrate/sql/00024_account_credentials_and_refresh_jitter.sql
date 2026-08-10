-- +goose Up
ALTER TABLE accounts
  ADD COLUMN has_login_credentials boolean NOT NULL DEFAULT false,
  ADD COLUMN session_refresh_jitter_seconds integer NOT NULL DEFAULT 0;

UPDATE accounts
SET credential_ciphertext='',
    has_login_credentials=false,
    session_refresh_jitter_seconds=mod(mod(hashtextextended(id::text,0),601)+601,601)::integer;

ALTER TABLE accounts
  ADD CONSTRAINT accounts_session_refresh_jitter_check
    CHECK(session_refresh_jitter_seconds BETWEEN 0 AND 600);

DROP INDEX IF EXISTS accounts_session_refresh_due_idx;
CREATE INDEX accounts_session_refresh_due_idx
  ON accounts(access_token_expires_at,session_refresh_jitter_seconds,id)
  WHERE session_refresh_enabled=true AND status<>'disabled';
CREATE INDEX accounts_admin_created_idx ON accounts(created_at,id);

-- +goose Down
ALTER TABLE accounts
  DROP CONSTRAINT IF EXISTS accounts_session_refresh_jitter_check,
  DROP COLUMN IF EXISTS session_refresh_jitter_seconds,
  DROP COLUMN IF EXISTS has_login_credentials;
DROP INDEX IF EXISTS accounts_admin_created_idx;
DROP INDEX IF EXISTS accounts_session_refresh_due_idx;
CREATE INDEX accounts_session_refresh_due_idx
  ON accounts(access_token_expires_at,id)
  WHERE session_refresh_enabled=true AND status<>'disabled';
