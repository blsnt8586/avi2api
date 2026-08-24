-- +goose Up
-- Align the Leonardo public catalog with the current upstream account policy.
-- Adobe mappings are intentionally left untouched; this is Leonardo-only.

INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('gemini-omni-flash','gemini-omni-flash','Gemini Omni Flash',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p"}'),
('happy-horse-1.1','happy-horse-1.1','Happy Horse 1.1',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
('kling-3.0','kling-3.0','Kling 3',true,'["text-to-video","image-to-video","native-audio"]','{"width":1920,"height":1080,"duration":5,"resolution":"1080p","generate_audio":true}'),
('kling-3.0-turbo','kling-3.0-turbo','Kling 3 Turbo',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
('hailuo-2.3','hailuo-2_3','Hailuo 2.3',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":6,"resolution":"768p"}'),
('wan-2.7','wan-2.7','Wan 2.7',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,
  enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults,updated_at=now();

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority,settings) VALUES
('leonardo','gemini-omni-flash','gemini-omni-flash',true,100,'{}'),
('leonardo','happy-horse-1.1','happy-horse-1.1',true,100,'{}'),
('leonardo','kling-3.0','kling-3.0',true,100,'{}'),
('leonardo','kling-3.0-turbo','kling-3.0-turbo',true,100,'{}'),
('leonardo','hailuo-2.3','hailuo-2_3',true,100,'{}'),
('leonardo','wan-2.7','wan-2.7',true,100,'{}')
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,
  priority=excluded.priority,settings=excluded.settings,updated_at=now();

UPDATE model_configs SET display_name='Kling 3 Omni',updated_at=now() WHERE id='kling-o3-omni';

-- Only the models shown by the current upstream account policy remain public.
UPDATE model_provider_configs
SET enabled=false,updated_at=now()
WHERE provider_id='leonardo'
  AND model_id NOT IN (
    'gpt-image-2','nano-banana-2','nano-banana-pro','seedream-5.0-pro',
    'gemini-omni-flash','happy-horse-1.1','kling-3.0','kling-3.0-turbo',
    'kling-o3-omni','hailuo-2.3','wan-2.7',
    'dialogue-v3','music-v1','sound-effects-v2'
  );

-- Keep historical price rows for accounting, but stop using hidden models for admission.
UPDATE model_cost_rules
SET enabled=false,updated_at=now()
WHERE provider_id='leonardo'
  AND model NOT IN (
    'gpt-image-2','nano-banana-2','nano-banana-pro','seedream-5.0-pro',
    'gemini-omni-flash','happy-horse-1.1','kling-3.0','kling-3.0-turbo',
    'kling-o3-omni','hailuo-2.3','wan-2.7',
    'dialogue-v3','music-v1','sound-effects-v2'
  );

-- Existing catalog-backed prices.
UPDATE model_cost_rules SET enabled=true,drifted=false,updated_at=now()
WHERE provider_id='leonardo' AND model IN ('gemini-omni-flash','kling-3.0');

-- New models use the latest catalog base-token rates until a dedicated price sample is recorded.
DELETE FROM model_cost_rules WHERE provider_id='leonardo' AND model IN ('happy-horse-1.1','kling-3.0-turbo','hailuo-2.3','wan-2.7');
INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source)
SELECT 'leonardo','video','happy-horse-1.1','','',r.resolution,duration,duration*r.rate,'catalog-2026-08-23','leonardo-catalog-base'
FROM (VALUES ('720p',150),('1080p',300)) r(resolution,rate) CROSS JOIN generate_series(3,15) duration;
INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source)
SELECT 'leonardo','video','kling-3.0-turbo','','',r.resolution,duration,duration*r.rate,'catalog-2026-08-23','leonardo-catalog-base'
FROM (VALUES ('720p',130),('1080p',260)) r(resolution,rate) CROSS JOIN generate_series(3,15) duration;
INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source)
SELECT 'leonardo','video','hailuo-2.3','','',r.resolution,duration,duration*r.rate,'catalog-2026-08-23','leonardo-catalog-base'
FROM (VALUES ('768p',98),('1080p',196)) r(resolution,rate) CROSS JOIN (VALUES (6),(10)) d(duration);
INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source)
SELECT 'leonardo','video','wan-2.7','','',r.resolution,duration,duration*r.rate,'catalog-2026-08-23','leonardo-catalog-base'
FROM (VALUES ('720p',45),('1080p',90)) r(resolution,rate) CROSS JOIN generate_series(2,10) duration;

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT
  '["leonardo:gpt-image-2","leonardo:nano-banana-2","leonardo:nano-banana-pro","leonardo:seedream-5.0-pro","leonardo:gemini-omni-flash","leonardo:happy-horse-1.1","leonardo:kling-3.0","leonardo:kling-3.0-turbo","leonardo:kling-o3-omni","leonardo:hailuo-2.3","leonardo:wan-2.7","leonardo:dialogue-v3","leonardo:music-v1","leonardo:sound-effects-v2"]'::jsonb;
UPDATE api_keys
SET allowed_models=(
  SELECT COALESCE(jsonb_agg(value ORDER BY value),'[]'::jsonb)
  FROM jsonb_array_elements_text(allowed_models) entry(value)
  WHERE value='*' OR value LIKE 'adobe:%' OR value IN (
    'leonardo:gpt-image-2','leonardo:nano-banana-2','leonardo:nano-banana-pro','leonardo:seedream-5.0-pro',
    'leonardo:gemini-omni-flash','leonardo:happy-horse-1.1','leonardo:kling-3.0','leonardo:kling-3.0-turbo',
    'leonardo:kling-o3-omni','leonardo:hailuo-2.3','leonardo:wan-2.7','leonardo:dialogue-v3',
    'leonardo:music-v1','leonardo:sound-effects-v2'
  )
)
WHERE deleted_at IS NULL AND NOT (allowed_models ? '*');

-- +goose Down
UPDATE model_provider_configs SET enabled=true,updated_at=now()
WHERE provider_id='leonardo' AND model_id IN ('flux-3-video','seedance-2.0','seedance-2.0-fast','seedance-2.0-mini','seedance-2.5','veo-3.1','veo-3.1-fast','kling-o3-omni','minimax-h3','grok-imagine-1.5');
UPDATE model_cost_rules SET enabled=true,updated_at=now()
WHERE provider_id='leonardo' AND model IN ('flux-3-video','seedance-2.0','seedance-2.0-fast','seedance-2.0-mini','seedance-2.5','veo-3.1','veo-3.1-fast','kling-o3-omni','minimax-h3','grok-imagine-1.5');
DELETE FROM model_cost_rules WHERE provider_id='leonardo' AND model IN ('happy-horse-1.1','kling-3.0-turbo','hailuo-2.3','wan-2.7');
DELETE FROM model_provider_configs WHERE provider_id='leonardo' AND model_id IN ('happy-horse-1.1','kling-3.0-turbo','hailuo-2.3','wan-2.7');
DELETE FROM model_configs WHERE id IN ('happy-horse-1.1','kling-3.0-turbo','hailuo-2.3','wan-2.7');
