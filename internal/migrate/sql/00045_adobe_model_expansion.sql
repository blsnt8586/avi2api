-- +goose Up
INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('adobe-gpt-image-2','gpt-image@2','Adobe GPT Image 2',true,'["text-to-image","image-to-image","quality"]','{"width":1024,"height":1024,"quantity":1,"quality":"low"}'),
('adobe-nano-banana-2','gemini-flash@nano-banana-3','Adobe Nano Banana 2',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
('adobe-kling-3.0-omni','kling@kling_v3_omni','Adobe Kling 3.0 Omni',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":false}'),
('adobe-veo-3.1','veo@3.1-generate','Adobe Veo 3.1',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":false}'),
('adobe-veo-3.1-fast','veo@3.1-fast-generate','Adobe Veo 3.1 Fast',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":false}'),
('adobe-seedance-2.0','seedance@seedance_2.0','Adobe Seedance 2.0',true,'["text-to-video","image-to-video","video-reference","audio-reference"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":false}'),
('adobe-seedance-2.0-fast','seedance@seedance_2.0_fast','Adobe Seedance 2.0 Fast',true,'["text-to-video","image-to-video","video-reference","audio-reference"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":false}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,
  enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults,updated_at=now();

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority,settings) VALUES
('adobe','adobe-gpt-image-2','gpt-image@2',true,200,'{"model_id":"gpt-image","model_version":"2","feature_id":"firefly_3p:external:gpt_image_2"}'),
('adobe','adobe-nano-banana-2','gemini-flash@nano-banana-3',true,200,'{"model_id":"gemini-flash","model_version":"nano-banana-3","feature_id":"firefly_3p:external:nano_banana_3"}'),
('adobe','adobe-kling-3.0-omni','kling@kling_v3_omni',true,200,'{"model_id":"kling","model_version":"kling_v3_omni","feature_id":"firefly_3p:external:kling_o3_tir2v"}'),
('adobe','adobe-veo-3.1','veo@3.1-generate',true,200,'{"model_id":"veo","model_version":"3.1-generate","feature_id":"firefly_3p:external:veo_3"}'),
('adobe','adobe-veo-3.1-fast','veo@3.1-fast-generate',true,200,'{"model_id":"veo","model_version":"3.1-fast-generate","feature_id":"firefly_3p:external:veo_3_fast"}'),
('adobe','adobe-seedance-2.0','seedance@seedance_2.0',true,200,'{"model_id":"seedance","model_version":"seedance_2.0","feature_id":"firefly_3p:external:seedance_2_0"}'),
('adobe','adobe-seedance-2.0-fast','seedance@seedance_2.0_fast',true,200,'{"model_id":"seedance","model_version":"seedance_2.0_fast","feature_id":"firefly_3p:external:seedance_2_0_fast"}')
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,
  priority=excluded.priority,settings=excluded.settings,updated_at=now();

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT
  '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","seedance-2.5","flux-3-video","veo-3.1","veo-3.1-fast","kling-o3-omni","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2","adobe-gpt-image-2","adobe-nano-banana-2","adobe-kling-3.0-omni","adobe-veo-3.1","adobe-veo-3.1-fast","adobe-seedance-2.0","adobe-seedance-2.0-fast"]'::jsonb;
UPDATE api_keys SET allowed_models=allowed_models || '["adobe-nano-banana-2"]'::jsonb WHERE NOT allowed_models ? 'adobe-nano-banana-2';
UPDATE api_keys SET allowed_models=allowed_models || '["adobe-kling-3.0-omni"]'::jsonb WHERE NOT allowed_models ? 'adobe-kling-3.0-omni';
UPDATE api_keys SET allowed_models=allowed_models || '["adobe-veo-3.1"]'::jsonb WHERE NOT allowed_models ? 'adobe-veo-3.1';
UPDATE api_keys SET allowed_models=allowed_models || '["adobe-seedance-2.0"]'::jsonb WHERE NOT allowed_models ? 'adobe-seedance-2.0';
UPDATE api_keys SET allowed_models=allowed_models || '["adobe-seedance-2.0-fast"]'::jsonb WHERE NOT allowed_models ? 'adobe-seedance-2.0-fast';

-- Adobe BKS prices are account-scoped and are synchronized after account validation.

-- +goose Down
DELETE FROM model_cost_rules WHERE provider_id='adobe' AND model IN ('adobe-nano-banana-2','adobe-kling-3.0-omni','adobe-veo-3.1','adobe-seedance-2.0','adobe-seedance-2.0-fast');
UPDATE api_keys SET allowed_models=allowed_models-'adobe-nano-banana-2'-'adobe-kling-3.0-omni'-'adobe-veo-3.1'-'adobe-seedance-2.0'-'adobe-seedance-2.0-fast';
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT
  '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","seedance-2.5","flux-3-video","veo-3.1","veo-3.1-fast","kling-o3-omni","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2","adobe-gpt-image-2","adobe-veo-3.1-fast"]'::jsonb;
DELETE FROM model_provider_configs WHERE provider_id='adobe' AND model_id IN ('adobe-nano-banana-2','adobe-kling-3.0-omni','adobe-veo-3.1','adobe-seedance-2.0','adobe-seedance-2.0-fast');
DELETE FROM model_configs WHERE id IN ('adobe-nano-banana-2','adobe-kling-3.0-omni','adobe-veo-3.1','adobe-seedance-2.0','adobe-seedance-2.0-fast');
