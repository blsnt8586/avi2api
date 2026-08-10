-- +goose Up
CREATE TABLE api_request_logs (
  id bigserial PRIMARY KEY,
  request_id text NOT NULL,
  api_key_id uuid REFERENCES api_keys(id) ON DELETE SET NULL,
  api_key_prefix text NOT NULL DEFAULT '',
  account_id uuid REFERENCES accounts(id) ON DELETE SET NULL,
  task_id uuid REFERENCES tasks(id) ON DELETE SET NULL,
  method text NOT NULL,
  path text NOT NULL,
  kind text NOT NULL DEFAULT '',
  model text NOT NULL DEFAULT '',
  parameters jsonb NOT NULL DEFAULT '{}'::jsonb,
  prompt_chars integer NOT NULL DEFAULT 0 CHECK (prompt_chars >= 0),
  estimated_tokens bigint CHECK (estimated_tokens IS NULL OR estimated_tokens >= 0),
  status integer NOT NULL CHECK (status BETWEEN 100 AND 599),
  error_code text NOT NULL DEFAULT '',
  duration_ms bigint NOT NULL CHECK (duration_ms >= 0),
  client_ip text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX api_request_logs_created_idx ON api_request_logs(created_at DESC,id DESC);
CREATE INDEX api_request_logs_status_idx ON api_request_logs(status,created_at DESC);
CREATE INDEX api_request_logs_model_idx ON api_request_logs(model,created_at DESC) WHERE model<>'';

-- +goose Down
DROP TABLE IF EXISTS api_request_logs;
