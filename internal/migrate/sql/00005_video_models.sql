-- +goose Up
INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('seedance-2.0','seedance-2.0','Seedance 2.0',true,'["text-to-video"]','{"width":1280,"height":720,"duration":8,"resolution":"720p"}'),
('seedance-2.0-fast','seedance-2.0-fast','Seedance 2.0 Fast',true,'["text-to-video"]','{"width":1280,"height":720,"duration":8,"resolution":"720p"}'),
('seedance-2.0-mini','seedance-2.0-mini','Seedance 2.0 Mini',true,'["text-to-video"]','{"width":1280,"height":720,"duration":8,"resolution":"720p"}'),
('gemini-omni-flash','gemini-omni-flash','Gemini Omni Flash',true,'["text-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p"}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults;

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash"]'::jsonb;
UPDATE api_keys SET allowed_models=allowed_models || '["seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash"]'::jsonb
WHERE NOT allowed_models ? 'seedance-2.0';

-- +goose Down
UPDATE api_keys SET allowed_models=allowed_models-'seedance-2.0'-'seedance-2.0-fast'-'seedance-2.0-mini'-'gemini-omni-flash';
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5"]'::jsonb;
DELETE FROM model_configs WHERE id IN ('seedance-2.0','seedance-2.0-fast','seedance-2.0-mini','gemini-omni-flash');
