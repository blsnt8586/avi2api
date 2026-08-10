-- +goose Up
ALTER TABLE accounts
  ADD COLUMN balance_refresh_version bigint NOT NULL DEFAULT 0 CHECK (balance_refresh_version >= 0);

WITH ranked AS (
  SELECT id,
    row_number() OVER (
      PARTITION BY kind,model,size,quality,resolution,duration
      ORDER BY updated_at DESC,id DESC
    ) AS position
  FROM model_cost_rules
  WHERE enabled=true
)
UPDATE model_cost_rules r
SET enabled=false,updated_at=now()
FROM ranked
WHERE r.id=ranked.id AND ranked.position>1;

CREATE UNIQUE INDEX model_cost_rules_one_enabled_idx
  ON model_cost_rules(kind,model,size,quality,resolution,duration)
  WHERE enabled=true;

-- +goose Down
DROP INDEX IF EXISTS model_cost_rules_one_enabled_idx;
ALTER TABLE accounts DROP COLUMN IF EXISTS balance_refresh_version;
