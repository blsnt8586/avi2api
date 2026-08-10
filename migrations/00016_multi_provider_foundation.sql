-- +goose Up
CREATE TABLE providers (
  id text PRIMARY KEY,
  display_name text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  auth_type text NOT NULL CHECK (auth_type IN ('browser_session','cookie','api_key','oauth')),
  credit_unit text NOT NULL DEFAULT 'credits',
  priority integer NOT NULL DEFAULT 100 CHECK (priority >= 0),
  capabilities jsonb NOT NULL DEFAULT '[]',
  settings jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO providers(id,display_name,auth_type,credit_unit,priority,capabilities,settings)
VALUES('leonardo','Leonardo AI','browser_session','credits',100,'["image","video","audio"]','{"login_url":"https://app.leonardo.ai/auth/login"}')
ON CONFLICT (id) DO NOTHING;

ALTER TABLE accounts
  ADD COLUMN provider_id text REFERENCES providers(id),
  ADD COLUMN credential_ciphertext text NOT NULL DEFAULT '',
  ADD COLUMN provider_metadata jsonb NOT NULL DEFAULT '{}';
UPDATE accounts SET provider_id='leonardo' WHERE provider_id IS NULL;
ALTER TABLE accounts ALTER COLUMN provider_id SET NOT NULL;
ALTER TABLE accounts ALTER COLUMN provider_id SET DEFAULT 'leonardo';
CREATE INDEX accounts_provider_status_idx ON accounts(provider_id,status);

CREATE TABLE model_provider_configs (
  provider_id text NOT NULL REFERENCES providers(id),
  model_id text NOT NULL REFERENCES model_configs(id) ON DELETE CASCADE,
  upstream_model text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  priority integer NOT NULL DEFAULT 100 CHECK (priority >= 0),
  settings jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(provider_id,model_id)
);
INSERT INTO model_provider_configs(provider_id,model_id,upstream_model)
SELECT 'leonardo',id,upstream_model FROM model_configs
ON CONFLICT (provider_id,model_id) DO NOTHING;

ALTER TABLE model_cost_rules ADD COLUMN provider_id text REFERENCES providers(id);
UPDATE model_cost_rules SET provider_id='leonardo' WHERE provider_id IS NULL;
ALTER TABLE model_cost_rules ALTER COLUMN provider_id SET NOT NULL;
ALTER TABLE model_cost_rules ALTER COLUMN provider_id SET DEFAULT 'leonardo';
-- +goose StatementBegin
DO $$
DECLARE unique_constraint record;
BEGIN
  FOR unique_constraint IN
    SELECT conname FROM pg_constraint
    WHERE conrelid='model_cost_rules'::regclass AND contype='u'
  LOOP
    EXECUTE format('ALTER TABLE model_cost_rules DROP CONSTRAINT %I', unique_constraint.conname);
  END LOOP;
END $$;
-- +goose StatementEnd
ALTER TABLE model_cost_rules
  ADD CONSTRAINT model_cost_rules_provider_version_key
  UNIQUE(provider_id,kind,model,size,quality,resolution,duration,price_version);
DROP INDEX IF EXISTS model_cost_rules_one_enabled_idx;
CREATE UNIQUE INDEX model_cost_rules_one_enabled_idx
  ON model_cost_rules(provider_id,kind,model,size,quality,resolution,duration)
  WHERE enabled=true;
CREATE INDEX model_cost_rules_provider_lookup_idx
  ON model_cost_rules(provider_id,kind,model,size,quality,resolution,duration)
  WHERE enabled=true;

ALTER TABLE tasks ADD COLUMN provider_id text REFERENCES providers(id);
UPDATE tasks t SET provider_id=a.provider_id FROM accounts a
WHERE t.account_id=a.id AND t.provider_id IS NULL;
UPDATE tasks SET provider_id='leonardo' WHERE provider_id IS NULL;
ALTER TABLE tasks ALTER COLUMN provider_id SET NOT NULL;
ALTER TABLE tasks ALTER COLUMN provider_id SET DEFAULT 'leonardo';
CREATE INDEX tasks_provider_status_idx ON tasks(provider_id,status,created_at);

-- +goose Down
DROP INDEX IF EXISTS tasks_provider_status_idx;
ALTER TABLE tasks DROP COLUMN IF EXISTS provider_id;
DROP INDEX IF EXISTS model_cost_rules_provider_lookup_idx;
DROP INDEX IF EXISTS model_cost_rules_one_enabled_idx;
CREATE UNIQUE INDEX model_cost_rules_one_enabled_idx
  ON model_cost_rules(kind,model,size,quality,resolution,duration)
  WHERE enabled=true;
ALTER TABLE model_cost_rules DROP COLUMN IF EXISTS provider_id;
DROP TABLE IF EXISTS model_provider_configs;
DROP INDEX IF EXISTS accounts_provider_status_idx;
ALTER TABLE accounts
  DROP COLUMN IF EXISTS provider_metadata,
  DROP COLUMN IF EXISTS credential_ciphertext,
  DROP COLUMN IF EXISTS provider_id;
DROP TABLE IF EXISTS providers;
