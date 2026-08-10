-- +goose Up
INSERT INTO settings(key,value) VALUES('schema_version','"1.247.2"')
ON CONFLICT(key) DO UPDATE SET value=excluded.value;

INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('grok-imagine-1.5','grok-imagine-1.5','Grok Imagine 1.5',true,'["image-to-video"]','{"width":736,"height":400,"duration":6,"resolution":"480p","generate_audio":true}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults;

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority)
VALUES('leonardo','grok-imagine-1.5','grok-imagine-1.5',true,100)
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,priority=excluded.priority,updated_at=now();

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","minimax-h3","grok-imagine-1.5","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
UPDATE api_keys SET allowed_models=allowed_models || '["grok-imagine-1.5"]'::jsonb
WHERE NOT allowed_models ? 'grok-imagine-1.5';

INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source) VALUES
('leonardo','video','grok-imagine-1.5','','','480p',3,300,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',4,400,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',5,500,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',6,600,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',7,700,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',8,800,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',9,900,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',10,1000,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',11,1100,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',12,1200,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',13,1300,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',14,1400,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','480p',15,1500,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',3,495,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',4,660,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',5,825,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',6,990,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',7,1155,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',8,1320,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',9,1485,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',10,1650,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',11,1815,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',12,1980,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',13,2145,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',14,2310,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','720p',15,2475,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',3,870,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',4,1160,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',5,1450,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',6,1740,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',7,2030,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',8,2320,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',9,2610,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',10,2900,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',11,3190,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',12,3480,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',13,3770,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',14,4060,'2026-08-04-schema-1.247.2','leonardo-schema'),
('leonardo','video','grok-imagine-1.5','','','1080p',15,4350,'2026-08-04-schema-1.247.2','leonardo-schema');

-- +goose Down
DELETE FROM model_cost_rules WHERE provider_id='leonardo' AND model='grok-imagine-1.5' AND price_version='2026-08-04-schema-1.247.2';
UPDATE api_keys SET allowed_models=allowed_models-'grok-imagine-1.5';
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-5.0-pro","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","minimax-h3","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
DELETE FROM model_provider_configs WHERE provider_id='leonardo' AND model_id='grok-imagine-1.5';
DELETE FROM model_configs WHERE id='grok-imagine-1.5';

