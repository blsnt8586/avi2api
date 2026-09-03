-- +goose Up

INSERT INTO settings(key, value)
VALUES ('schema_version', '"1.280.1"')
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = now();

UPDATE model_configs
SET defaults = defaults || '{"style_ids":["111dc692-d470-4eec-b791-3475abac4c46"],"prompt_enhance":"AUTO"}'::jsonb,
    updated_at = now()
WHERE id = 'gpt-image-2';

-- +goose Down
UPDATE settings SET value = '"1.258.0"', updated_at = now() WHERE key = 'schema_version';
UPDATE model_configs
SET defaults = defaults - 'style_ids' - 'prompt_enhance', updated_at = now()
WHERE id = 'gpt-image-2';
