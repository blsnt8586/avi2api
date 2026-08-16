-- +goose Up
INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('kling-3.0-omni','kling@kling_v3_omni','Kling 3.0 Omni',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":false}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,
  enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults,updated_at=now();

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority,settings) VALUES
('adobe','gpt-image-2','gpt-image@2',true,200,'{"model_id":"gpt-image","model_version":"2","feature_id":"firefly_3p:external:gpt_image_2"}'),
('adobe','nano-banana-2','gemini-flash@nano-banana-3',true,200,'{"model_id":"gemini-flash","model_version":"nano-banana-3","feature_id":"firefly_3p:external:nano_banana_3"}'),
('adobe','kling-3.0-omni','kling@kling_v3_omni',true,200,'{"model_id":"kling","model_version":"kling_v3_omni","feature_id":"firefly_3p:external:kling_o3_tir2v"}'),
('adobe','veo-3.1','veo@3.1-generate',true,200,'{"model_id":"veo","model_version":"3.1-generate","feature_id":"firefly_3p:external:veo_3"}'),
('adobe','veo-3.1-fast','veo@3.1-fast-generate',true,200,'{"model_id":"veo","model_version":"3.1-fast-generate","feature_id":"firefly_3p:external:veo_3_fast"}'),
('adobe','seedance-2.0','seedance@seedance_2.0',true,200,'{"model_id":"seedance","model_version":"seedance_2.0","feature_id":"firefly_3p:external:seedance_2_0"}'),
('adobe','seedance-2.0-fast','seedance@seedance_2.0_fast',true,200,'{"model_id":"seedance","model_version":"seedance_2.0_fast","feature_id":"firefly_3p:external:seedance_2_0_fast"}')
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,
  priority=excluded.priority,settings=excluded.settings,updated_at=now();

UPDATE model_cost_rules
SET model=CASE model
  WHEN 'adobe-gpt-image-2' THEN 'gpt-image-2'
  WHEN 'adobe-nano-banana-2' THEN 'nano-banana-2'
  WHEN 'adobe-kling-3.0-omni' THEN 'kling-3.0-omni'
  WHEN 'adobe-veo-3.1' THEN 'veo-3.1'
  WHEN 'adobe-veo-3.1-fast' THEN 'veo-3.1-fast'
  WHEN 'adobe-seedance-2.0' THEN 'seedance-2.0'
  WHEN 'adobe-seedance-2.0-fast' THEN 'seedance-2.0-fast'
  ELSE model END,
  updated_at=now()
WHERE provider_id='adobe' AND model LIKE 'adobe-%';

UPDATE tasks
SET model=substring(model FROM 7),
  request=jsonb_set(request,'{model}',to_jsonb(substring(model FROM 7)),true),
  updated_at=now()
WHERE provider_id='adobe' AND model LIKE 'adobe-%';

UPDATE api_keys
SET allowed_models=(
  SELECT COALESCE(jsonb_agg(DISTINCT CASE
    WHEN value='*' THEN '*'
    WHEN value LIKE 'adobe-%' THEN 'adobe:' || substring(value FROM 7)
    ELSE 'leonardo:' || value
  END),'[]'::jsonb)
  FROM jsonb_array_elements_text(allowed_models) entry(value)
);

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT
  '["leonardo:gpt-image-2","leonardo:nano-banana-2","leonardo:nano-banana-pro","leonardo:seedream-5.0-pro","leonardo:seedance-2.0","leonardo:seedance-2.0-fast","leonardo:seedance-2.0-mini","leonardo:seedance-2.5","leonardo:flux-3-video","leonardo:veo-3.1","leonardo:veo-3.1-fast","leonardo:kling-o3-omni","leonardo:minimax-h3","leonardo:grok-imagine-1.5","leonardo:dialogue-v3","leonardo:music-v1","leonardo:sound-effects-v2","adobe:gpt-image-2","adobe:nano-banana-2","adobe:kling-3.0-omni","adobe:veo-3.1","adobe:veo-3.1-fast","adobe:seedance-2.0","adobe:seedance-2.0-fast"]'::jsonb;

DELETE FROM model_provider_configs
WHERE provider_id='adobe' AND model_id IN ('adobe-gpt-image-2','adobe-nano-banana-2','adobe-kling-3.0-omni','adobe-veo-3.1','adobe-veo-3.1-fast','adobe-seedance-2.0','adobe-seedance-2.0-fast');
DELETE FROM model_configs
WHERE id IN ('adobe-gpt-image-2','adobe-nano-banana-2','adobe-kling-3.0-omni','adobe-veo-3.1','adobe-veo-3.1-fast','adobe-seedance-2.0','adobe-seedance-2.0-fast');

-- +goose Down
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

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority,settings)
SELECT provider_id,'adobe-' || model_id,upstream_model,enabled,priority,settings
FROM model_provider_configs
WHERE provider_id='adobe' AND model_id IN ('gpt-image-2','nano-banana-2','kling-3.0-omni','veo-3.1','veo-3.1-fast','seedance-2.0','seedance-2.0-fast')
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=excluded.enabled,
  priority=excluded.priority,settings=excluded.settings,updated_at=now();

UPDATE model_cost_rules SET model='adobe-' || model,updated_at=now()
WHERE provider_id='adobe' AND model IN ('gpt-image-2','nano-banana-2','kling-3.0-omni','veo-3.1','veo-3.1-fast','seedance-2.0','seedance-2.0-fast');
UPDATE tasks
SET model='adobe-' || model,
  request=jsonb_set(request,'{model}',to_jsonb('adobe-' || model),true),
  updated_at=now()
WHERE provider_id='adobe' AND model IN ('gpt-image-2','nano-banana-2','kling-3.0-omni','veo-3.1','veo-3.1-fast','seedance-2.0','seedance-2.0-fast');

UPDATE api_keys
SET allowed_models=(
  SELECT COALESCE(jsonb_agg(DISTINCT CASE
    WHEN value='*' THEN '*'
    WHEN value LIKE 'adobe:%' THEN 'adobe-' || substring(value FROM 7)
    WHEN value LIKE 'leonardo:%' THEN substring(value FROM 10)
    ELSE value
  END),'[]'::jsonb)
  FROM jsonb_array_elements_text(allowed_models) entry(value)
);
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT
  '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","seedance-2.5","flux-3-video","veo-3.1","veo-3.1-fast","kling-o3-omni","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2","adobe-gpt-image-2","adobe-nano-banana-2","adobe-kling-3.0-omni","adobe-veo-3.1","adobe-veo-3.1-fast","adobe-seedance-2.0","adobe-seedance-2.0-fast"]'::jsonb;

DELETE FROM model_provider_configs
WHERE provider_id='adobe' AND model_id IN ('gpt-image-2','nano-banana-2','kling-3.0-omni','veo-3.1','veo-3.1-fast','seedance-2.0','seedance-2.0-fast');
DELETE FROM model_configs WHERE id='kling-3.0-omni';
