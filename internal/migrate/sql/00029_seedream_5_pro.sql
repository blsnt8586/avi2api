-- +goose Up
INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('seedream-5.0-pro','seedream-5.0-pro','Seedream 5.0 Pro',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults;

UPDATE model_configs SET enabled=false WHERE id='seedream-4.5';
UPDATE model_provider_configs SET enabled=false WHERE model_id='seedream-4.5';
INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority)
VALUES('leonardo','seedream-5.0-pro','seedream-5.0-pro',true,100)
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,priority=excluded.priority,updated_at=now();

UPDATE api_keys
SET allowed_models=(allowed_models-'seedream-4.5') || '["seedream-5.0-pro"]'::jsonb
WHERE allowed_models ? 'seedream-4.5' AND NOT allowed_models ? 'seedream-5.0-pro';
UPDATE api_keys
SET allowed_models=allowed_models-'seedream-4.5'
WHERE allowed_models ? 'seedream-4.5' AND allowed_models ? 'seedream-5.0-pro';
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","minimax-h3","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;

UPDATE model_cost_rules SET enabled=false WHERE provider_id='leonardo' AND model='seedream-4.5' AND enabled=true;
INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source) VALUES
('leonardo','image','seedream-5.0-pro','1024x1024','','',0,45,'2026-08-01-schema-1.247.2','leonardo-schema'),
('leonardo','image','seedream-5.0-pro','2048x2048','','',0,90,'2026-08-01-schema-1.247.2','leonardo-schema');

-- +goose Down
DELETE FROM model_cost_rules WHERE provider_id='leonardo' AND model='seedream-5.0-pro' AND price_version='2026-08-01-schema-1.247.2';
UPDATE model_cost_rules SET enabled=true WHERE provider_id='leonardo' AND model='seedream-4.5';
UPDATE api_keys
SET allowed_models=(allowed_models-'seedream-5.0-pro') || '["seedream-4.5"]'::jsonb
WHERE allowed_models ? 'seedream-5.0-pro' AND NOT allowed_models ? 'seedream-4.5';
UPDATE api_keys
SET allowed_models=allowed_models-'seedream-5.0-pro'
WHERE allowed_models ? 'seedream-5.0-pro' AND allowed_models ? 'seedream-4.5';
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","minimax-h3","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
UPDATE model_provider_configs SET enabled=true WHERE provider_id='leonardo' AND model_id='seedream-4.5';
DELETE FROM model_provider_configs WHERE provider_id='leonardo' AND model_id='seedream-5.0-pro';
DELETE FROM model_configs WHERE id='seedream-5.0-pro';
UPDATE model_configs SET enabled=true WHERE id='seedream-4.5';
