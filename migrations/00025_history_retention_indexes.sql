-- +goose Up
CREATE INDEX audit_logs_retention_idx ON audit_logs(created_at,id);
CREATE INDEX task_events_retention_idx ON task_events(created_at,id);
CREATE INDEX task_outbox_retention_idx ON task_outbox(updated_at,task_id)
  WHERE delivered_at IS NOT NULL;
CREATE INDEX session_refresh_jobs_retention_idx ON session_refresh_jobs(updated_at,id)
  WHERE status IN ('succeeded','cancelled');
CREATE INDEX account_reconciliation_batches_retention_idx
  ON account_reconciliation_batches(created_at,id);

-- +goose Down
DROP INDEX IF EXISTS account_reconciliation_batches_retention_idx;
DROP INDEX IF EXISTS session_refresh_jobs_retention_idx;
DROP INDEX IF EXISTS task_outbox_retention_idx;
DROP INDEX IF EXISTS task_events_retention_idx;
DROP INDEX IF EXISTS audit_logs_retention_idx;
