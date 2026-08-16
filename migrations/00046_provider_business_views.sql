-- +goose Up
ALTER TABLE platform_model_catalog ADD COLUMN provider_id text REFERENCES providers(id);
UPDATE platform_model_catalog SET provider_id='leonardo' WHERE provider_id IS NULL;
ALTER TABLE platform_model_catalog ALTER COLUMN provider_id SET NOT NULL;
ALTER TABLE platform_model_catalog ALTER COLUMN provider_id SET DEFAULT 'leonardo';

ALTER TABLE platform_model_catalog DROP CONSTRAINT platform_model_catalog_pkey;
ALTER TABLE platform_model_catalog ADD PRIMARY KEY(provider_id,media_type,upstream_id);
DROP INDEX IF EXISTS platform_model_catalog_media_order_idx;
CREATE INDEX platform_model_catalog_provider_media_order_idx
  ON platform_model_catalog(provider_id,media_type,sort_order,upstream_id);

UPDATE providers SET settings=settings || '{"catalog_sync":true}'::jsonb,updated_at=now()
WHERE id='leonardo';
UPDATE providers SET settings=settings || '{"catalog_sync":false}'::jsonb,updated_at=now()
WHERE id='adobe';

-- +goose Down
DROP INDEX IF EXISTS platform_model_catalog_provider_media_order_idx;
ALTER TABLE platform_model_catalog DROP CONSTRAINT platform_model_catalog_pkey;
ALTER TABLE platform_model_catalog DROP COLUMN provider_id;
ALTER TABLE platform_model_catalog ADD PRIMARY KEY(media_type,upstream_id);
CREATE INDEX platform_model_catalog_media_order_idx
  ON platform_model_catalog(media_type,sort_order,upstream_id);
UPDATE providers SET settings=settings-'catalog_sync',updated_at=now()
WHERE id IN ('leonardo','adobe');
