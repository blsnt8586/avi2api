-- +goose Up
INSERT INTO model_configs(id,upstream_model,display_name,capabilities,defaults) VALUES
('gpt-image-2','gpt-image-2','GPT Image 2 via Leonardo','["text-to-image","image-to-image","quality"]','{"width":1024,"height":1024,"quantity":1}'),
('leonardo-lucid-origin','lucid-origin','Leonardo Lucid Origin','["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
('flux-2-pro','flux-pro-2.0','FLUX.2 Pro via Leonardo','["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
('nano-banana-2','nano-banana-2','Nano Banana 2 via Leonardo','["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,capabilities=excluded.capabilities,defaults=excluded.defaults;

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["leonardo-auto","gpt-image-2","leonardo-lucid-origin","flux-2-pro","nano-banana-2"]'::jsonb;
UPDATE api_keys SET allowed_models = allowed_models || '["gpt-image-2","leonardo-lucid-origin","flux-2-pro","nano-banana-2"]'::jsonb
WHERE NOT allowed_models ? 'gpt-image-2';

-- +goose Down
DELETE FROM model_configs WHERE id IN ('gpt-image-2','leonardo-lucid-origin','flux-2-pro','nano-banana-2');
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["leonardo-auto"]'::jsonb;
