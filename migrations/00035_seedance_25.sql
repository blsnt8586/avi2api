-- +goose Up
INSERT INTO settings(key,value) VALUES('schema_version','"1.255.2"')
ON CONFLICT(key) DO UPDATE SET value=excluded.value;

INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('seedance-2.5','bytedance/seedance-2.5','Seedance 2.5',true,'["text-to-video","image-to-video","video-reference","audio-reference"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":true}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults;

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority)
VALUES('leonardo','seedance-2.5','bytedance/seedance-2.5',true,100)
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,priority=excluded.priority,updated_at=now();

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","seedance-2.5","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
UPDATE api_keys SET allowed_models=allowed_models || '["seedance-2.5"]'::jsonb
WHERE NOT allowed_models ? 'seedance-2.5';

INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source)
SELECT 'leonardo','video','seedance-2.5','','',rates.resolution,duration,duration*rates.tokens_per_second,'2026-08-09-schema-1.255.2','leonardo-schema'
FROM (VALUES ('480p',180),('720p',292)) AS rates(resolution,tokens_per_second)
CROSS JOIN generate_series(4,30) AS duration;

-- +goose Down
DELETE FROM model_cost_rules WHERE provider_id='leonardo' AND model='seedance-2.5' AND price_version='2026-08-09-schema-1.255.2';
UPDATE api_keys SET allowed_models=allowed_models-'seedance-2.5';
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
DELETE FROM model_provider_configs WHERE provider_id='leonardo' AND model_id='seedance-2.5';
DELETE FROM model_configs WHERE id='seedance-2.5';
UPDATE settings SET value='"1.247.2"' WHERE key='schema_version' AND value='"1.255.2"';
