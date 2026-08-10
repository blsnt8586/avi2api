-- +goose Up
CREATE TABLE system_capacity_config (
  id smallint PRIMARY KEY DEFAULT 1 CHECK(id=1),
  max_executing integer NOT NULL DEFAULT 100 CHECK(max_executing BETWEEN 1 AND 10000),
  max_queued integer NOT NULL DEFAULT 1000 CHECK(max_queued BETWEEN 1 AND 100000),
  queue_high_watermark integer NOT NULL DEFAULT 900,
  queue_resume_watermark integer NOT NULL DEFAULT 700,
  queue_timeout_seconds integer NOT NULL DEFAULT 1800 CHECK(queue_timeout_seconds BETWEEN 60 AND 86400),
  maintenance_mode boolean NOT NULL DEFAULT false,
  execution_paused boolean NOT NULL DEFAULT false,
  overload_active boolean NOT NULL DEFAULT false,
  revision bigint NOT NULL DEFAULT 1 CHECK(revision > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK(queue_high_watermark BETWEEN 1 AND max_queued),
  CHECK(queue_resume_watermark >= 0 AND queue_resume_watermark < queue_high_watermark),
  CHECK(NOT execution_paused OR maintenance_mode)
);

INSERT INTO system_capacity_config(id) VALUES(1) ON CONFLICT DO NOTHING;

CREATE INDEX tasks_kind_active_idx
  ON tasks(kind,created_at,id)
  WHERE status IN ('reserving','uploading','submitted','polling');

-- +goose Down
DROP INDEX IF EXISTS tasks_kind_active_idx;
DROP TABLE IF EXISTS system_capacity_config;
