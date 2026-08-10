-- name: GetTask :one
SELECT * FROM tasks WHERE id = $1;

-- name: ListActiveAccounts :many
SELECT * FROM accounts WHERE status = 'active' AND (cooldown_until IS NULL OR cooldown_until <= now());

-- name: ListModels :many
SELECT * FROM model_configs WHERE enabled = true ORDER BY id;
