-- +goose Up
UPDATE model_configs SET enabled=false,updated_at=now() WHERE id='seedance-2.5';
UPDATE model_provider_configs SET enabled=false,updated_at=now() WHERE provider_id='leonardo' AND model_id='seedance-2.5';
UPDATE model_cost_rules SET enabled=false,updated_at=now() WHERE provider_id='leonardo' AND model='seedance-2.5';

INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('flux-3-video','bfl/flux-3-video','FLUX 3 Video',true,'["text-to-video","image-to-video","video-reference","native-audio"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":true}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults,updated_at=now();

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority)
VALUES('leonardo','flux-3-video','bfl/flux-3-video',true,100)
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,priority=excluded.priority,updated_at=now();

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","flux-3-video","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
UPDATE api_keys
SET allowed_models=(allowed_models-'seedance-2.5') ||
  CASE WHEN allowed_models ? 'flux-3-video' THEN '[]'::jsonb ELSE '["flux-3-video"]'::jsonb END
WHERE allowed_models ? 'seedance-2.5' OR NOT allowed_models ? 'flux-3-video';

INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source)
SELECT 'leonardo','video','flux-3-video','','',rates.resolution,duration,duration*rates.tokens_per_second,'2026-08-09-schema-1.255.2-flux3','leonardo-schema'
FROM (VALUES ('720p',215),('1080p',366)) AS rates(resolution,tokens_per_second)
CROSS JOIN generate_series(5,20) AS duration;

-- +goose Down
DELETE FROM model_cost_rules WHERE provider_id='leonardo' AND model='flux-3-video' AND price_version='2026-08-09-schema-1.255.2-flux3';
DELETE FROM model_provider_configs WHERE provider_id='leonardo' AND model_id='flux-3-video';
DELETE FROM model_configs WHERE id='flux-3-video';

UPDATE model_configs SET enabled=true,updated_at=now() WHERE id='seedance-2.5';
UPDATE model_provider_configs SET enabled=true,updated_at=now() WHERE provider_id='leonardo' AND model_id='seedance-2.5';
UPDATE model_cost_rules SET enabled=true,updated_at=now() WHERE provider_id='leonardo' AND model='seedance-2.5' AND price_version='2026-08-09-schema-1.255.2';

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","seedance-2.5","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
UPDATE api_keys
SET allowed_models=(allowed_models-'flux-3-video') ||
  CASE WHEN allowed_models ? 'seedance-2.5' THEN '[]'::jsonb ELSE '["seedance-2.5"]'::jsonb END
WHERE allowed_models ? 'flux-3-video' OR NOT allowed_models ? 'seedance-2.5';
