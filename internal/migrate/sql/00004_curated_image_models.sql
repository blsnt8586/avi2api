-- +goose Up
INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('nano-banana-pro','gemini-image-2','Nano Banana Pro',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
('seedream-4.5','seedream-4.5','Seedream 4.5',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults;

UPDATE model_configs SET enabled = id IN ('gpt-image-2','nano-banana-2','nano-banana-pro','seedream-4.5');
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5"]'::jsonb;
UPDATE api_keys SET allowed_models='["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5"]'::jsonb;

-- +goose Down
UPDATE model_configs SET enabled=true WHERE id IN ('leonardo-auto','leonardo-lucid-origin','flux-2-pro');
DELETE FROM model_configs WHERE id IN ('nano-banana-pro','seedream-4.5');
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["leonardo-auto","gpt-image-2","leonardo-lucid-origin","flux-2-pro","nano-banana-2"]'::jsonb;
UPDATE api_keys SET allowed_models='["leonardo-auto","gpt-image-2","leonardo-lucid-origin","flux-2-pro","nano-banana-2"]'::jsonb;
