-- +goose Up
ALTER TABLE api_request_logs ADD COLUMN provider_id text REFERENCES providers(id);

UPDATE api_request_logs l
SET provider_id=COALESCE(
  (SELECT t.provider_id FROM tasks t WHERE t.id=l.task_id),
  (SELECT a.provider_id FROM accounts a WHERE a.id=l.account_id),
  NULLIF(l.parameters->>'provider',''),
  'leonardo'
);

ALTER TABLE api_request_logs ALTER COLUMN provider_id SET NOT NULL;
ALTER TABLE api_request_logs ALTER COLUMN provider_id SET DEFAULT 'leonardo';
CREATE INDEX api_request_logs_provider_created_idx ON api_request_logs(provider_id,created_at DESC,id DESC);
UPDATE providers SET credit_unit='bks',updated_at=now() WHERE id='adobe';

-- +goose Down
DROP INDEX IF EXISTS api_request_logs_provider_created_idx;
ALTER TABLE api_request_logs DROP COLUMN provider_id;
UPDATE providers SET credit_unit='credits',updated_at=now() WHERE id='adobe';
