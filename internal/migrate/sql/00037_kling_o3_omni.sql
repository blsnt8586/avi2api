-- +goose Up
UPDATE model_configs
SET enabled=false,updated_at=now()
WHERE id IN ('veo-3.1-lite','gemini-omni-flash','kling-3.0');

UPDATE model_provider_configs
SET enabled=false,updated_at=now()
WHERE provider_id='leonardo' AND model_id IN ('veo-3.1-lite','gemini-omni-flash','kling-3.0');

UPDATE model_cost_rules
SET enabled=false,updated_at=now()
WHERE provider_id='leonardo' AND model IN ('veo-3.1-lite','gemini-omni-flash','kling-3.0');

INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('kling-o3-omni','kling-video-o-3','Kling Video O3 Omni',true,'["text-to-video","image-to-video","video-reference","native-audio"]','{"width":1920,"height":1080,"duration":5,"resolution":"1080p","generate_audio":true}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults,updated_at=now();

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority)
VALUES('leonardo','kling-o3-omni','kling-video-o-3',true,100)
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,priority=excluded.priority,updated_at=now();

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","flux-3-video","veo-3.1","veo-3.1-fast","kling-o3-omni","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
UPDATE api_keys
SET allowed_models=allowed_models-'veo-3.1-lite'-'gemini-omni-flash'-'kling-3.0' ||
  CASE WHEN allowed_models ? 'kling-o3-omni' THEN '[]'::jsonb ELSE '["kling-o3-omni"]'::jsonb END
WHERE allowed_models ? 'veo-3.1-lite' OR allowed_models ? 'gemini-omni-flash' OR allowed_models ? 'kling-3.0' OR NOT allowed_models ? 'kling-o3-omni';

INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source)
SELECT 'leonardo','video','kling-o3-omni','','',rates.resolution,duration,duration*rates.tokens_per_second,'2026-08-09-schema-1.255.2-kling-o3','leonardo-schema'
FROM (VALUES ('720p',224),('1080p',280),('2160p',420)) AS rates(resolution,tokens_per_second)
CROSS JOIN generate_series(3,15) AS duration;

-- +goose Down
DELETE FROM model_cost_rules WHERE provider_id='leonardo' AND model='kling-o3-omni' AND price_version='2026-08-09-schema-1.255.2-kling-o3';
DELETE FROM model_provider_configs WHERE provider_id='leonardo' AND model_id='kling-o3-omni';
DELETE FROM model_configs WHERE id='kling-o3-omni';

UPDATE model_configs
SET enabled=true,updated_at=now()
WHERE id IN ('veo-3.1-lite','gemini-omni-flash','kling-3.0');

UPDATE model_provider_configs
SET enabled=true,updated_at=now()
WHERE provider_id='leonardo' AND model_id IN ('veo-3.1-lite','gemini-omni-flash','kling-3.0');

UPDATE model_cost_rules
SET enabled=true,updated_at=now()
WHERE provider_id='leonardo' AND (
  (model='gemini-omni-flash' AND price_version='2026-07-15-ui') OR
  (model IN ('veo-3.1-lite','kling-3.0') AND price_version='2026-07-25-schema-1.232.1')
);

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","flux-3-video","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
UPDATE api_keys
SET allowed_models=allowed_models-'kling-o3-omni' ||
  CASE WHEN allowed_models ? 'veo-3.1-lite' THEN '[]'::jsonb ELSE '["veo-3.1-lite"]'::jsonb END ||
  CASE WHEN allowed_models ? 'gemini-omni-flash' THEN '[]'::jsonb ELSE '["gemini-omni-flash"]'::jsonb END ||
  CASE WHEN allowed_models ? 'kling-3.0' THEN '[]'::jsonb ELSE '["kling-3.0"]'::jsonb END
WHERE allowed_models ? 'kling-o3-omni' OR NOT allowed_models ? 'veo-3.1-lite' OR NOT allowed_models ? 'gemini-omni-flash' OR NOT allowed_models ? 'kling-3.0';
