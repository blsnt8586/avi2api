-- +goose Up
ALTER TABLE accounts
  ADD COLUMN IF NOT EXISTS last_submitted_at timestamptz;

ALTER TABLE tasks
  ADD COLUMN IF NOT EXISTS execution_lease_id uuid,
  ADD COLUMN IF NOT EXISTS execution_lease_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS execution_attempt integer NOT NULL DEFAULT 0 CHECK (execution_attempt >= 0);

CREATE TABLE task_outbox (
  task_id uuid PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
  kind text NOT NULL CHECK (kind IN ('image', 'video', 'audio')),
  delivered_at timestamptz,
  last_attempt_at timestamptz,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX account_reservations_held_account_idx
  ON account_reservations(account_id) WHERE state='held';
CREATE INDEX task_outbox_dispatch_idx
  ON task_outbox(next_attempt_at, created_at) WHERE delivered_at IS NULL;
CREATE INDEX tasks_execution_lease_idx
  ON tasks(execution_lease_expires_at)
  WHERE status IN ('reserving', 'uploading', 'submitted', 'polling');

-- Backfill existing work so a restart can safely redispatch it through the Outbox.
INSERT INTO task_outbox(task_id, kind, delivered_at)
SELECT id, kind, CASE WHEN status='queued' THEN NULL ELSE now() END
FROM tasks
WHERE status IN ('queued', 'reserving', 'uploading', 'submitted', 'polling')
ON CONFLICT (task_id) DO NOTHING;

UPDATE tasks
SET execution_lease_id=gen_random_uuid(), execution_lease_expires_at=now()
WHERE status IN ('reserving', 'uploading', 'submitted', 'polling')
  AND execution_lease_id IS NULL;

-- +goose Down
DROP TABLE IF EXISTS task_outbox;
DROP INDEX IF EXISTS tasks_execution_lease_idx;
DROP INDEX IF EXISTS account_reservations_held_account_idx;
ALTER TABLE tasks
  DROP COLUMN IF EXISTS execution_attempt,
  DROP COLUMN IF EXISTS execution_lease_expires_at,
  DROP COLUMN IF EXISTS execution_lease_id;
ALTER TABLE accounts DROP COLUMN IF EXISTS last_submitted_at;
