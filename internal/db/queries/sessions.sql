-- name: CreateSession :one
INSERT INTO sessions (session_id, user_id, user_agent, expires_at, last_seen_at, absolute_expires_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE session_id = ? LIMIT 1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE session_id = ?;

-- name: DeleteSessionByIDAndUser :execrows
DELETE FROM sessions WHERE session_id = ? AND user_id = ?;

-- name: TouchSession :exec
UPDATE sessions SET expires_at = ?, last_seen_at = ? WHERE session_id = ?;

-- name: ListSessionsByUser :many
SELECT s.*, u.username FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.user_id = ? ORDER BY s.last_seen_at DESC;

-- name: DeleteSessionsByUser :execrows
DELETE FROM sessions WHERE user_id = ? AND session_id != ?;

-- name: PurgeExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < ? OR absolute_expires_at < ?;
