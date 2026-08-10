-- +goose Up
CREATE TABLE admin_credentials (
  username text PRIMARY KEY,
  password_salt bytea NOT NULL,
  password_hash bytea NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE admin_credentials;
