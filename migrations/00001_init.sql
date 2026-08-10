-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE accounts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  email text NOT NULL DEFAULT '',
  cookie_ciphertext text NOT NULL,
  access_token_ciphertext text NOT NULL DEFAULT '',
  access_token_expires_at timestamptz,
  hasura_user_id text NOT NULL DEFAULT '',
  cognito_sub text NOT NULL DEFAULT '',
  team_id text NOT NULL DEFAULT '',
  plan text NOT NULL DEFAULT '',
  subscription_tokens bigint NOT NULL DEFAULT 0,
  rollover_tokens bigint NOT NULL DEFAULT 0,
  paid_tokens bigint NOT NULL DEFAULT 0,
  proxy_url text NOT NULL DEFAULT '',
  user_agent text NOT NULL DEFAULT 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0.0.0 Safari/537.36',
  image_concurrency integer NOT NULL DEFAULT 1 CHECK (image_concurrency > 0),
  status text NOT NULL DEFAULT 'active',
  cooldown_until timestamptz,
  last_error text NOT NULL DEFAULT '',
  last_checked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE api_keys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  key_prefix text NOT NULL,
  key_hash bytea NOT NULL UNIQUE,
  enabled boolean NOT NULL DEFAULT true,
  concurrency_limit integer NOT NULL DEFAULT 20,
  allowed_models jsonb NOT NULL DEFAULT '["leonardo-auto"]',
  request_count bigint NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz
);

CREATE TABLE model_configs (
  id text PRIMARY KEY,
  upstream_model text NOT NULL,
  display_name text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  capabilities jsonb NOT NULL DEFAULT '["text-to-image"]',
  defaults jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tasks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  api_key_id uuid REFERENCES api_keys(id),
  account_id uuid REFERENCES accounts(id),
  kind text NOT NULL DEFAULT 'image',
  status text NOT NULL,
  progress integer NOT NULL DEFAULT 0,
  model text NOT NULL,
  prompt text NOT NULL,
  request jsonb NOT NULL,
  request_hash bytea NOT NULL,
  idempotency_key text,
  generation_id text NOT NULL DEFAULT '',
  result jsonb NOT NULL DEFAULT '{}',
  error_code text NOT NULL DEFAULT '',
  error_message text NOT NULL DEFAULT '',
  retry_count integer NOT NULL DEFAULT 0,
  tokens_before bigint,
  tokens_after bigint,
  cancel_requested boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  completed_at timestamptz,
  UNIQUE(api_key_id, idempotency_key)
);

CREATE INDEX tasks_status_created_idx ON tasks(status, created_at);
CREATE INDEX tasks_generation_idx ON tasks(generation_id) WHERE generation_id <> '';

CREATE TABLE task_events (
  id bigserial PRIMARY KEY,
  task_id uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  status text NOT NULL,
  message text NOT NULL DEFAULT '',
  payload jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE settings (
  key text PRIMARY KEY,
  value jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_logs (
  id bigserial PRIMARY KEY,
  actor text NOT NULL,
  action text NOT NULL,
  target text NOT NULL DEFAULT '',
  metadata jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO model_configs(id, upstream_model, display_name, capabilities, defaults)
VALUES ('leonardo-auto', 'auto-preset', 'Leonardo Auto', '["text-to-image"]', '{"width":1024,"height":1024,"quantity":1,"style_ids":["111dc692-d470-4eec-b791-3475abac4c46"]}')
ON CONFLICT DO NOTHING;

INSERT INTO settings(key, value) VALUES ('schema_version', '"1.232.1"') ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS audit_logs, settings, task_events, tasks, model_configs, api_keys, accounts CASCADE;
