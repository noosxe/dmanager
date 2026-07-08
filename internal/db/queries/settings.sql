-- name: GetSetting :one
SELECT * FROM settings WHERE key = ? LIMIT 1;

-- name: UpdateSetting :exec
INSERT INTO settings (key, value, updated_at)
VALUES (?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(key) DO UPDATE SET
    value = excluded.value,
    updated_at = CURRENT_TIMESTAMP;

-- name: ListSettings :many
SELECT * FROM settings;
