-- +goose Up
INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('veo-3.1','veo-3.1-generate-001','Veo 3.1',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":true}'),
('veo-3.1-fast','veo-3.1-fast-generate-001','Veo 3.1 Fast',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":true}'),
('veo-3.1-lite','veo-3.1-lite','Veo 3.1 Lite',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":true}'),
('kling-3.0','kling-3.0','Kling Video 3.0',true,'["text-to-video","image-to-video"]','{"width":1920,"height":1080,"duration":5,"resolution":"1080p","generate_audio":true}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults;

INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority)
VALUES
('leonardo','veo-3.1','veo-3.1-generate-001',true,100),
('leonardo','veo-3.1-fast','veo-3.1-fast-generate-001',true,100),
('leonardo','veo-3.1-lite','veo-3.1-lite',true,100),
('leonardo','kling-3.0','kling-3.0',true,100)
ON CONFLICT(provider_id,model_id) DO UPDATE SET upstream_model=excluded.upstream_model,enabled=true,priority=excluded.priority,updated_at=now();

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash","veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
UPDATE api_keys SET allowed_models=allowed_models || '["veo-3.1","veo-3.1-fast","veo-3.1-lite","kling-3.0"]'::jsonb
WHERE NOT allowed_models ? 'veo-3.1' OR NOT allowed_models ? 'veo-3.1-fast' OR NOT allowed_models ? 'veo-3.1-lite' OR NOT allowed_models ? 'kling-3.0';

INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,price_version,source) VALUES
('leonardo','video','veo-3.1','','','720p',4,1600,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1','','','720p',6,2400,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1','','','720p',8,3200,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1','','','1080p',4,1600,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1','','','1080p',6,2400,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1','','','1080p',8,3200,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1','','','2160p',4,3200,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1','','','2160p',6,4800,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1','','','2160p',8,6400,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-fast','','','720p',4,600,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-fast','','','720p',6,900,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-fast','','','720p',8,1200,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-fast','','','1080p',4,600,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-fast','','','1080p',6,900,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-fast','','','1080p',8,1200,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-fast','','','2160p',4,1400,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-fast','','','2160p',6,2100,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-fast','','','2160p',8,2800,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-lite','','','720p',4,200,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-lite','','','720p',6,300,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-lite','','','720p',8,400,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-lite','','','1080p',4,320,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-lite','','','1080p',6,480,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','veo-3.1-lite','','','1080p',8,640,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',3,378,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',4,504,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',5,630,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',6,756,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',7,882,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',8,1008,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',9,1134,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',10,1260,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',11,1386,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',12,1512,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',13,1638,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',14,1764,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','720p',15,1890,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',3,504,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',4,672,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',5,840,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',6,1008,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',7,1176,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',8,1344,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',9,1512,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',10,1680,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',11,1848,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',12,2016,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',13,2184,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',14,2352,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','1080p',15,2520,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',3,1260,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',4,1680,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',5,2100,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',6,2520,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',7,2940,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',8,3360,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',9,3780,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',10,4200,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',11,4620,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',12,5040,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',13,5460,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',14,5880,'2026-07-25-schema-1.232.1','leonardo-schema'),
('leonardo','video','kling-3.0','','','2160p',15,6300,'2026-07-25-schema-1.232.1','leonardo-schema');

-- +goose Down
DELETE FROM model_cost_rules WHERE provider_id='leonardo' AND price_version='2026-07-25-schema-1.232.1' AND model IN ('veo-3.1','veo-3.1-fast','veo-3.1-lite','kling-3.0');
UPDATE api_keys SET allowed_models=allowed_models-'veo-3.1'-'veo-3.1-fast'-'veo-3.1-lite'-'kling-3.0';
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
DELETE FROM model_configs WHERE id IN ('veo-3.1','veo-3.1-fast','veo-3.1-lite','kling-3.0');
