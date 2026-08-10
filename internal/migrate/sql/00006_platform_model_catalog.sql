-- +goose Up
CREATE TABLE platform_model_catalog (
  media_type text NOT NULL CHECK (media_type IN ('image', 'video')),
  upstream_id text NOT NULL,
  schema_version text NOT NULL,
  sort_order integer NOT NULL DEFAULT 0,
  model_data jsonb NOT NULL,
  synced_at timestamptz NOT NULL,
  PRIMARY KEY (media_type, upstream_id)
);

CREATE INDEX platform_model_catalog_media_order_idx
  ON platform_model_catalog(media_type, sort_order, upstream_id);

-- +goose Down
DROP TABLE platform_model_catalog;
