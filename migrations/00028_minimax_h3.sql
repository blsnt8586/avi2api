-- +goose Up
INSERT INTO settings(key,value) VALUES('schema_version','"1.247.2"')
ON CONFLICT(key) DO UPDATE SET value=excluded.value;

INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('minimax-h3','hailuo-03','MiniMax H3',true,'["text-to-video","image-to-video"]','{"width":2560,"height":1440,"duration":5,"resolution":"1440p","generate_audio":true}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults;

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority)
VALUES('leonardo','minimax-h3','hailuo-03',true,100)
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,priority=excluded.priority,updated_at=now();

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","minimax-h3","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
UPDATE api_keys SET allowed_models=allowed_models || '["minimax-h3"]'::jsonb
WHERE NOT allowed_models ? 'minimax-h3';

INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source) VALUES
('leonardo','video','minimax-h3','','','1440p',5,700,'2026-07-31-schema-1.247.2','leonardo-schema'),
('leonardo','video','minimax-h3','','','1440p',6,840,'2026-07-31-schema-1.247.2','leonardo-schema'),
('leonardo','video','minimax-h3','','','1440p',7,980,'2026-07-31-schema-1.247.2','leonardo-schema'),
('leonardo','video','minimax-h3','','','1440p',8,1120,'2026-07-31-schema-1.247.2','leonardo-schema'),
('leonardo','video','minimax-h3','','','1440p',9,1260,'2026-07-31-schema-1.247.2','leonardo-schema'),
('leonardo','video','minimax-h3','','','1440p',10,1400,'2026-07-31-schema-1.247.2','leonardo-schema'),
('leonardo','video','minimax-h3','','','1440p',11,1540,'2026-07-31-schema-1.247.2','leonardo-schema'),
('leonardo','video','minimax-h3','','','1440p',12,1680,'2026-07-31-schema-1.247.2','leonardo-schema'),
('leonardo','video','minimax-h3','','','1440p',13,1820,'2026-07-31-schema-1.247.2','leonardo-schema'),
('leonardo','video','minimax-h3','','','1440p',14,1960,'2026-07-31-schema-1.247.2','leonardo-schema'),
('leonardo','video','minimax-h3','','','1440p',15,2100,'2026-07-31-schema-1.247.2','leonardo-schema');

-- +goose Down
DELETE FROM model_cost_rules WHERE provider_id='leonardo' AND model='minimax-h3' AND price_version='2026-07-31-schema-1.247.2';
UPDATE api_keys SET allowed_models=allowed_models-'minimax-h3';
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
DELETE FROM model_provider_configs WHERE provider_id='leonardo' AND model_id='minimax-h3';
DELETE FROM model_configs WHERE id='minimax-h3';
UPDATE settings SET value='"1.232.1"' WHERE key='schema_version' AND value='"1.247.2"';
