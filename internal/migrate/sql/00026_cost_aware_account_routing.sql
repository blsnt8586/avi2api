-- +goose Up
ALTER TABLE accounts
  ADD COLUMN routing_role text NOT NULL DEFAULT 'general',
  ADD COLUMN protected_tokens bigint NOT NULL DEFAULT 0,
  ADD COLUMN video_reserved_slots integer NOT NULL DEFAULT 0;

ALTER TABLE accounts
  ADD CONSTRAINT accounts_routing_role_check
    CHECK(routing_role IN ('general','video_reserved')),
  ADD CONSTRAINT accounts_protected_tokens_check
    CHECK(protected_tokens>=0),
  ADD CONSTRAINT accounts_video_reserved_slots_check
    CHECK(video_reserved_slots>=0 AND video_reserved_slots<=image_concurrency);

ALTER TABLE accounts ALTER COLUMN queue_capacity SET DEFAULT 40;
UPDATE accounts SET queue_capacity=GREATEST(queue_capacity,40);

WITH video_floor AS (
  SELECT COALESCE((
    SELECT unit_tokens FROM model_cost_rules
    WHERE provider_id='leonardo' AND kind='video' AND model='seedance-2.0'
      AND resolution='1080p' AND duration=10 AND enabled=true AND drifted=false
    ORDER BY verified_at DESC,id DESC LIMIT 1
  ),6804)::bigint AS tokens
), selected AS (
  SELECT id FROM accounts
  WHERE provider_id='leonardo' AND status<>'disabled'
    AND subscription_tokens+rollover_tokens+paid_tokens>=(SELECT tokens FROM video_floor)
  ORDER BY subscription_tokens+rollover_tokens+paid_tokens DESC,created_at DESC,id
  LIMIT 4
)
UPDATE accounts a SET
  routing_role='video_reserved',
  protected_tokens=(SELECT tokens FROM video_floor),
  video_reserved_slots=LEAST(1,image_concurrency),
  queue_capacity=20,
  updated_at=now()
FROM selected s WHERE a.id=s.id;

CREATE INDEX accounts_routing_role_idx
  ON accounts(provider_id,routing_role,status,id);

-- +goose Down
DROP INDEX IF EXISTS accounts_routing_role_idx;
ALTER TABLE accounts ALTER COLUMN queue_capacity SET DEFAULT 5;
ALTER TABLE accounts
  DROP CONSTRAINT IF EXISTS accounts_video_reserved_slots_check,
  DROP CONSTRAINT IF EXISTS accounts_protected_tokens_check,
  DROP CONSTRAINT IF EXISTS accounts_routing_role_check,
  DROP COLUMN IF EXISTS video_reserved_slots,
  DROP COLUMN IF EXISTS protected_tokens,
  DROP COLUMN IF EXISTS routing_role;
