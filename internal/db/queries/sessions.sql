-- name: CreateSession :one
INSERT INTO sessions (session_id, user_id, expires_at)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE session_id = ? LIMIT 1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE session_id = ?;

-- name: PurgeExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < ?;
