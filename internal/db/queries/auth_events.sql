-- name: CreateAuthEvent :one
INSERT INTO auth_events (user_id, username, event, detail)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListAuthEvents :many
SELECT * FROM auth_events
ORDER BY created_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: ListAuthEventsByUser :many
SELECT * FROM auth_events
WHERE user_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: CountAuthEvents :one
SELECT COUNT(*) FROM auth_events;

-- name: CountAuthEventsByUser :one
SELECT COUNT(*) FROM auth_events
WHERE user_id = ?;

-- name: PurgeExpiredAuthEvents :exec
DELETE FROM auth_events WHERE created_at < ?;
