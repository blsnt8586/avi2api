-- +goose Up
CREATE INDEX audit_logs_created_id_desc_idx
  ON audit_logs(created_at DESC,id DESC);

-- +goose Down
DROP INDEX IF EXISTS audit_logs_created_id_desc_idx;
