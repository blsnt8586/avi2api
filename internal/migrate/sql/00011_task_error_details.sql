-- +goose Up
ALTER TABLE tasks ADD COLUMN error_details jsonb NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE tasks DROP COLUMN error_details;
