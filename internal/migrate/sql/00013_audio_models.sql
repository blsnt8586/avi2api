-- +goose Up
ALTER TABLE platform_model_catalog DROP CONSTRAINT IF EXISTS platform_model_catalog_media_type_check;
ALTER TABLE platform_model_catalog
  ADD CONSTRAINT platform_model_catalog_media_type_check CHECK (media_type IN ('image', 'video', 'audio'));

ALTER TABLE model_cost_rules DROP CONSTRAINT IF EXISTS model_cost_rules_kind_check;
ALTER TABLE model_cost_rules
  ADD CONSTRAINT model_cost_rules_kind_check CHECK (kind IN ('image', 'video', 'audio'));

INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
('dialogue-v3','dialogue-v3','Dialogue',true,'["text-to-speech"]','{"quantity":1,"voice":"george","language":"en","prompt_influence":0.5}'),
('music-v1','music-v1','Music',true,'["text-to-music"]','{"quantity":1,"duration_minutes":1,"force_instrumental":false}'),
('sound-effects-v2','sound-effects-v2','Sound Effects',true,'["text-to-sound"]','{"quantity":1,"duration":2,"loop":false,"prompt_influence":0.7}')
ON CONFLICT(id) DO UPDATE SET upstream_model=excluded.upstream_model,display_name=excluded.display_name,enabled=true,capabilities=excluded.capabilities,defaults=excluded.defaults;

ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash","dialogue-v3","music-v1","sound-effects-v2"]'::jsonb;
UPDATE api_keys
SET allowed_models=allowed_models || '["dialogue-v3","music-v1","sound-effects-v2"]'::jsonb
WHERE NOT allowed_models ? 'dialogue-v3';

INSERT INTO model_cost_rules(kind,model,size,quality,resolution,duration,unit_tokens,price_version,source) VALUES
('audio','dialogue-v3','','','',0,90,'2026-07-23-schema','leonardo-schema'),
('audio','music-v1','','','',1,700,'2026-07-23-schema','leonardo-schema'),
('audio','music-v1','','','',2,1400,'2026-07-23-schema','leonardo-schema'),
('audio','music-v1','','','',3,2100,'2026-07-23-schema','leonardo-schema'),
('audio','music-v1','','','',4,2800,'2026-07-23-schema','leonardo-schema'),
('audio','music-v1','','','',5,3500,'2026-07-23-schema','leonardo-schema'),
('audio','music-v1','','','',6,4200,'2026-07-23-schema','leonardo-schema'),
('audio','music-v1','','','',7,4900,'2026-07-23-schema','leonardo-schema'),
('audio','music-v1','','','',8,5600,'2026-07-23-schema','leonardo-schema'),
('audio','music-v1','','','',9,6300,'2026-07-23-schema','leonardo-schema'),
('audio','music-v1','','','',10,7000,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',1,2,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',2,4,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',3,6,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',4,8,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',5,10,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',6,12,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',7,14,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',8,16,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',9,18,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',10,20,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',11,22,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',12,24,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',13,26,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',14,28,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',15,30,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',16,32,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',17,34,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',18,36,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',19,38,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',20,40,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',21,42,'2026-07-23-schema','leonardo-schema'),
('audio','sound-effects-v2','','','',22,44,'2026-07-23-schema','leonardo-schema');

-- +goose Down
DELETE FROM model_cost_rules WHERE kind='audio' AND price_version='2026-07-23-schema';
UPDATE api_keys SET allowed_models=allowed_models-'dialogue-v3'-'music-v1'-'sound-effects-v2';
ALTER TABLE api_keys ALTER COLUMN allowed_models SET DEFAULT '["gpt-image-2","nano-banana-2","nano-banana-pro","seedream-4.5","seedance-2.0","seedance-2.0-fast","seedance-2.0-mini","gemini-omni-flash"]'::jsonb;
DELETE FROM model_configs WHERE id IN ('dialogue-v3','music-v1','sound-effects-v2');
ALTER TABLE model_cost_rules DROP CONSTRAINT IF EXISTS model_cost_rules_kind_check;
ALTER TABLE model_cost_rules ADD CONSTRAINT model_cost_rules_kind_check CHECK (kind IN ('image','video'));
ALTER TABLE platform_model_catalog DROP CONSTRAINT IF EXISTS platform_model_catalog_media_type_check;
ALTER TABLE platform_model_catalog ADD CONSTRAINT platform_model_catalog_media_type_check CHECK (media_type IN ('image','video'));
