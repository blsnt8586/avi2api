-- +goose Up
ALTER TABLE tasks
  ADD COLUMN queue_deadline_at timestamptz,
  ADD COLUMN upstream_deadline_at timestamptz,
  ADD COLUMN last_upstream_status_at timestamptz,
  ADD COLUMN unknown_status_count integer NOT NULL DEFAULT 0 CHECK(unknown_status_count >= 0),
  ADD COLUMN reconciliation_reason text NOT NULL DEFAULT '';

UPDATE tasks
SET queue_deadline_at=created_at+interval '30 minutes'
WHERE queue_deadline_at IS NULL AND status='queued';

UPDATE tasks
SET upstream_deadline_at=COALESCE(submitted_at,started_at,updated_at)+interval '30 minutes'
WHERE upstream_deadline_at IS NULL
  AND status IN ('submitted','polling')
  AND generation_id<>'';

CREATE INDEX tasks_queue_deadline_idx
  ON tasks(queue_deadline_at)
  WHERE status='queued';
CREATE INDEX tasks_upstream_deadline_idx
  ON tasks(upstream_deadline_at)
  WHERE status IN ('submitted','polling') AND generation_id<>'';
CREATE INDEX tasks_provider_uncertain_idx
  ON tasks(provider_id,account_id,completed_at)
  WHERE status='submission_uncertain';

-- +goose Down
DROP INDEX IF EXISTS tasks_provider_uncertain_idx;
DROP INDEX IF EXISTS tasks_upstream_deadline_idx;
DROP INDEX IF EXISTS tasks_queue_deadline_idx;
ALTER TABLE tasks
  DROP COLUMN IF EXISTS reconciliation_reason,
  DROP COLUMN IF EXISTS unknown_status_count,
  DROP COLUMN IF EXISTS last_upstream_status_at,
  DROP COLUMN IF EXISTS upstream_deadline_at,
  DROP COLUMN IF EXISTS queue_deadline_at;
