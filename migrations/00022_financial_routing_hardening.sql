-- +goose Up
ALTER TABLE accounts
  ADD COLUMN balance_snapshot_version bigint NOT NULL DEFAULT 0 CHECK (balance_snapshot_version >= 0),
  ADD COLUMN balance_snapshot_started_at timestamptz;

ALTER TABLE tasks
  ADD COLUMN submitted_at timestamptz;

UPDATE tasks t
SET submitted_at=COALESCE(
  (SELECT min(e.created_at) FROM task_events e WHERE e.task_id=t.id AND e.status='submitted'),
  t.started_at,
  t.updated_at
)
WHERE t.submitted_at IS NULL
  AND (t.generation_id<>'' OR t.status IN ('submitted','polling','submission_uncertain'));

UPDATE accounts
SET balance_snapshot_version=balance_refresh_version,
    balance_snapshot_started_at=last_checked_at
WHERE balance_snapshot_version=0;

ALTER TABLE account_reservations
  ADD COLUMN settled_tokens bigint CHECK (settled_tokens >= 0),
  ADD COLUMN reconciled_snapshot_version bigint CHECK (reconciled_snapshot_version >= 0);

UPDATE account_reservations
SET settled_tokens=estimated_tokens,
    reconciled_snapshot_version=0
WHERE state='consumed';

UPDATE account_reservations
SET settled_tokens=0,
    reconciled_snapshot_version=0
WHERE state='released';

UPDATE account_reservations
SET released_at=COALESCE(released_at,updated_at)
WHERE state<>'held';

UPDATE account_reservations
SET released_at=NULL
WHERE state='held';

ALTER TABLE model_cost_rules
  ADD COLUMN drifted boolean NOT NULL DEFAULT false,
  ADD COLUMN drift_reason text NOT NULL DEFAULT '',
  ADD COLUMN verified_at timestamptz NOT NULL DEFAULT now();

CREATE TABLE account_reconciliation_batches (
  id bigserial PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES accounts(id),
  snapshot_version bigint NOT NULL CHECK(snapshot_version >= 0),
  snapshot_started_at timestamptz NOT NULL,
  baseline_tokens bigint NOT NULL CHECK(baseline_tokens >= 0),
  observed_tokens bigint NOT NULL CHECK(observed_tokens >= 0),
  observed_spend bigint NOT NULL CHECK(observed_spend >= 0),
  expected_tokens bigint NOT NULL CHECK(expected_tokens >= 0),
  reservation_count integer NOT NULL CHECK(reservation_count >= 0),
  unresolved_tokens bigint NOT NULL CHECK(unresolved_tokens >= 0),
  unresolved_count integer NOT NULL CHECK(unresolved_count >= 0),
  candidate_signature text NOT NULL,
  status text NOT NULL CHECK(status IN ('baseline','ok','pending','drift')),
  details jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(account_id,snapshot_version)
);

CREATE TABLE api_key_usage_ledger (
  id bigserial PRIMARY KEY,
  task_id uuid NOT NULL UNIQUE REFERENCES tasks(id),
  api_key_id uuid REFERENCES api_keys(id),
  provider_id text NOT NULL REFERENCES providers(id),
  account_id uuid NOT NULL REFERENCES accounts(id),
  pricing_rule_id bigint REFERENCES model_cost_rules(id),
  kind text NOT NULL CHECK(kind IN ('image','video','audio')),
  model text NOT NULL,
  estimated_tokens bigint NOT NULL CHECK(estimated_tokens >= 0),
  settled_tokens bigint NOT NULL CHECK(settled_tokens >= 0),
  settlement_state text NOT NULL CHECK(settlement_state IN ('released','consumed')),
  release_reason text NOT NULL,
  settled_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO api_key_usage_ledger(
  task_id,api_key_id,provider_id,account_id,pricing_rule_id,kind,model,
  estimated_tokens,settled_tokens,settlement_state,release_reason,settled_at
)
SELECT t.id,t.api_key_id,t.provider_id,r.account_id,t.pricing_rule_id,t.kind,t.model,
  r.estimated_tokens,COALESCE(r.settled_tokens,CASE WHEN r.state='consumed' THEN r.estimated_tokens ELSE 0 END),
  r.state,r.release_reason,COALESCE(r.released_at,r.updated_at)
FROM account_reservations r
JOIN tasks t ON t.id=r.task_id
WHERE r.state IN ('released','consumed');

-- +goose StatementBegin
CREATE FUNCTION append_api_key_usage_ledger() RETURNS trigger AS $$
BEGIN
  IF OLD.state='held' AND NEW.state IN ('released','consumed') THEN
    INSERT INTO api_key_usage_ledger(
      task_id,api_key_id,provider_id,account_id,pricing_rule_id,kind,model,
      estimated_tokens,settled_tokens,settlement_state,release_reason,settled_at
    )
    SELECT t.id,t.api_key_id,t.provider_id,NEW.account_id,t.pricing_rule_id,t.kind,t.model,
      NEW.estimated_tokens,NEW.settled_tokens,NEW.state,NEW.release_reason,NEW.released_at
    FROM tasks t WHERE t.id=NEW.task_id;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION enforce_reservation_immutability() RETURNS trigger AS $$
BEGIN
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'account reservation history is immutable';
  END IF;
  IF OLD.task_id IS DISTINCT FROM NEW.task_id OR OLD.estimated_tokens IS DISTINCT FROM NEW.estimated_tokens THEN
    RAISE EXCEPTION 'account reservation financial identity is immutable';
  END IF;
  IF OLD.state<>'held' AND NEW IS DISTINCT FROM OLD THEN
    RAISE EXCEPTION 'settled account reservation is immutable';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION prevent_usage_ledger_mutation() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'API key usage ledger is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER account_reservations_immutability
  BEFORE UPDATE OR DELETE ON account_reservations
  FOR EACH ROW EXECUTE FUNCTION enforce_reservation_immutability();
CREATE TRIGGER account_reservations_usage_ledger
  AFTER UPDATE ON account_reservations
  FOR EACH ROW EXECUTE FUNCTION append_api_key_usage_ledger();
CREATE TRIGGER api_key_usage_ledger_immutability
  BEFORE UPDATE OR DELETE ON api_key_usage_ledger
  FOR EACH ROW EXECUTE FUNCTION prevent_usage_ledger_mutation();

CREATE INDEX account_reconciliation_batches_account_idx
  ON account_reconciliation_batches(account_id,snapshot_version DESC);
CREATE INDEX api_key_usage_ledger_key_time_idx
  ON api_key_usage_ledger(api_key_id,settled_at DESC,id DESC);
CREATE INDEX api_key_usage_ledger_account_time_idx
  ON api_key_usage_ledger(account_id,settled_at DESC,id DESC);
CREATE INDEX accounts_routing_fresh_idx
  ON accounts(provider_id,last_checked_at,id)
  WHERE status='active';
CREATE INDEX tasks_account_active_idx
  ON tasks(account_id,created_at,id)
  WHERE status IN ('reserving','uploading','submitted','polling');
CREATE INDEX tasks_account_queued_idx
  ON tasks(account_id,created_at,id)
  WHERE status='queued';
CREATE INDEX tasks_api_key_active_idx
  ON tasks(api_key_id,created_at,id)
  WHERE status IN ('reserving','uploading','submitted','polling');
CREATE INDEX tasks_created_desc_idx ON tasks(created_at DESC);
CREATE INDEX tasks_account_id_idx ON tasks(account_id);
CREATE INDEX tasks_account_submitted_idx ON tasks(account_id,submitted_at,id)
  WHERE submitted_at IS NOT NULL;
CREATE INDEX tasks_pricing_rule_id_idx ON tasks(pricing_rule_id);
CREATE INDEX task_events_task_id_idx ON task_events(task_id);

CREATE UNIQUE INDEX accounts_id_provider_unique_idx ON accounts(id,provider_id);
CREATE UNIQUE INDEX model_cost_rules_id_provider_unique_idx ON model_cost_rules(id,provider_id);
CREATE UNIQUE INDEX tasks_financial_identity_unique_idx
  ON tasks(id,account_id,estimated_tokens);

ALTER TABLE tasks
  ADD CONSTRAINT tasks_account_provider_fk
    FOREIGN KEY(account_id,provider_id) REFERENCES accounts(id,provider_id) NOT VALID,
  ADD CONSTRAINT tasks_pricing_provider_fk
    FOREIGN KEY(pricing_rule_id,provider_id) REFERENCES model_cost_rules(id,provider_id) NOT VALID,
  ADD CONSTRAINT tasks_status_check
    CHECK(status IN ('queued','reserving','uploading','submitted','polling','succeeded','failed','cancelled','submission_uncertain')) NOT VALID;

ALTER TABLE account_reservations
  ADD CONSTRAINT account_reservations_task_identity_fk
    FOREIGN KEY(task_id,account_id,estimated_tokens)
    REFERENCES tasks(id,account_id,estimated_tokens) NOT VALID,
  ADD CONSTRAINT account_reservations_terminal_fields_check
    CHECK(
      (state='held' AND released_at IS NULL AND settled_tokens IS NULL AND reconciled_snapshot_version IS NULL)
      OR
      (state IN ('released','consumed') AND released_at IS NOT NULL AND settled_tokens IS NOT NULL AND reconciled_snapshot_version IS NOT NULL)
    ) NOT VALID;

ALTER TABLE tasks VALIDATE CONSTRAINT tasks_account_provider_fk;
ALTER TABLE tasks VALIDATE CONSTRAINT tasks_pricing_provider_fk;
ALTER TABLE tasks VALIDATE CONSTRAINT tasks_status_check;
ALTER TABLE account_reservations VALIDATE CONSTRAINT account_reservations_task_identity_fk;
ALTER TABLE account_reservations VALIDATE CONSTRAINT account_reservations_terminal_fields_check;

-- +goose Down
DROP TRIGGER IF EXISTS api_key_usage_ledger_immutability ON api_key_usage_ledger;
DROP TRIGGER IF EXISTS account_reservations_usage_ledger ON account_reservations;
DROP TRIGGER IF EXISTS account_reservations_immutability ON account_reservations;
DROP FUNCTION IF EXISTS prevent_usage_ledger_mutation();
DROP FUNCTION IF EXISTS append_api_key_usage_ledger();
DROP FUNCTION IF EXISTS enforce_reservation_immutability();

ALTER TABLE account_reservations
  DROP CONSTRAINT IF EXISTS account_reservations_terminal_fields_check,
  DROP CONSTRAINT IF EXISTS account_reservations_task_identity_fk;
ALTER TABLE tasks
  DROP CONSTRAINT IF EXISTS tasks_status_check,
  DROP CONSTRAINT IF EXISTS tasks_pricing_provider_fk,
  DROP CONSTRAINT IF EXISTS tasks_account_provider_fk;

DROP INDEX IF EXISTS tasks_financial_identity_unique_idx;
DROP INDEX IF EXISTS model_cost_rules_id_provider_unique_idx;
DROP INDEX IF EXISTS accounts_id_provider_unique_idx;
DROP INDEX IF EXISTS task_events_task_id_idx;
DROP INDEX IF EXISTS tasks_pricing_rule_id_idx;
DROP INDEX IF EXISTS tasks_account_submitted_idx;
DROP INDEX IF EXISTS tasks_account_id_idx;
DROP INDEX IF EXISTS tasks_created_desc_idx;
DROP INDEX IF EXISTS tasks_api_key_active_idx;
DROP INDEX IF EXISTS tasks_account_queued_idx;
DROP INDEX IF EXISTS tasks_account_active_idx;
DROP INDEX IF EXISTS accounts_routing_fresh_idx;
DROP INDEX IF EXISTS api_key_usage_ledger_account_time_idx;
DROP INDEX IF EXISTS api_key_usage_ledger_key_time_idx;
DROP TABLE IF EXISTS api_key_usage_ledger;
DROP TABLE IF EXISTS account_reconciliation_batches;

ALTER TABLE model_cost_rules
  DROP COLUMN IF EXISTS verified_at,
  DROP COLUMN IF EXISTS drift_reason,
  DROP COLUMN IF EXISTS drifted;
ALTER TABLE account_reservations
  DROP COLUMN IF EXISTS reconciled_snapshot_version,
  DROP COLUMN IF EXISTS settled_tokens;
ALTER TABLE accounts
  DROP COLUMN IF EXISTS balance_snapshot_started_at,
  DROP COLUMN IF EXISTS balance_snapshot_version;
ALTER TABLE tasks DROP COLUMN IF EXISTS submitted_at;
