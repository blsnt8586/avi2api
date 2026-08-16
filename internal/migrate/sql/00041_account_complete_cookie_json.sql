-- +goose Up
ALTER TABLE accounts
  ADD COLUMN cookie_json_ciphertext text NOT NULL DEFAULT '',
  ADD COLUMN pending_cookie_json_ciphertext text NOT NULL DEFAULT '',
  ADD COLUMN pending_cookie_json_fingerprint text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE accounts
  DROP COLUMN pending_cookie_json_fingerprint,
  DROP COLUMN pending_cookie_json_ciphertext,
  DROP COLUMN cookie_json_ciphertext;
