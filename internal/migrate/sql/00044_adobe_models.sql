-- +goose Up
UPDATE providers SET auth_type='oauth',enabled=true,capabilities='["image","video"]',
  settings=settings || '{"models_enabled":true}'::jsonb,updated_at=now()
WHERE id='adobe';

INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('adobe-gpt-image-2','gpt-image@2','Adobe GPT Image 2',true,'["text-to-image","image-to-image","quality"]','{"width":1024,"height":1024,"quantity":1,"quality":"low"}'),
('adobe-veo-3.1-fast','veo@3.1-fast-generate','Adobe Veo 3.1 Fast',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":8,"resolution":"720p"}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,
  enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults,updated_at=now();

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority,settings) VALUES
('adobe','adobe-gpt-image-2','gpt-image@2',true,200,'{"model_id":"gpt-image","model_version":"2","feature_id":"firefly_3p:external:gpt_image_2"}'),
('adobe','adobe-veo-3.1-fast','veo@3.1-fast-generate',true,200,'{"model_id":"veo","model_version":"3.1-fast-generate","feature_id":"firefly_3p:external:veo_3_fast"}')
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,
  priority=excluded.priority,settings=excluded.settings,updated_at=now();

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT
  '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","seedance-2.5","flux-3-video","veo-3.1","veo-3.1-fast","kling-o3-omni","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2","adobe-gpt-image-2","adobe-veo-3.1-fast"]'::jsonb;
UPDATE api_keys SET allowed_models=allowed_models || '["adobe-gpt-image-2","adobe-veo-3.1-fast"]'::jsonb
WHERE NOT allowed_models ? 'adobe-gpt-image-2' OR NOT allowed_models ? 'adobe-veo-3.1-fast';

-- Adobe BKS prices are account-scoped and are synchronized after account validation.

-- +goose Down
DELETE FROM model_cost_rules WHERE provider_id='adobe' AND model IN ('adobe-gpt-image-2','adobe-veo-3.1-fast');
UPDATE api_keys SET allowed_models=allowed_models-'adobe-gpt-image-2'-'adobe-veo-3.1-fast';
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT
  '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","seedance-2.5","flux-3-video","veo-3.1","veo-3.1-fast","kling-o3-omni","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
DELETE FROM model_provider_configs WHERE provider_id='adobe' AND model_id IN ('adobe-gpt-image-2','adobe-veo-3.1-fast');
DELETE FROM model_configs WHERE id IN ('adobe-gpt-image-2','adobe-veo-3.1-fast');
UPDATE providers SET settings=settings || '{"models_enabled":false}'::jsonb,updated_at=now() WHERE id='adobe';
