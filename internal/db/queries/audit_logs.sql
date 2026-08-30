-- name: CreateAuditLog :one
INSERT INTO audit_logs (actor, actor_role, source, action, resource_type, resource_id, outcome, detail)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- Filters are optional: an empty query string disables the substring match,
-- empty source/outcome disable those filters. COALESCE keeps NULL params
-- (zero-value sql.NullString) behaving like empty strings, not SQL NULL.
-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE
  (
    COALESCE(?, '') = ''
    OR actor LIKE '%' || COALESCE(?, '') || '%'
    OR action LIKE '%' || COALESCE(?, '') || '%'
    OR resource_id LIKE '%' || COALESCE(?, '') || '%'
    OR detail LIKE '%' || COALESCE(?, '') || '%'
  )
  AND (COALESCE(?, '') = '' OR source = COALESCE(?, ''))
  AND (COALESCE(?, '') = '' OR outcome = COALESCE(?, ''))
ORDER BY created_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: CountAuditLogs :one
SELECT COUNT(*) FROM audit_logs
WHERE
  (
    COALESCE(?, '') = ''
    OR actor LIKE '%' || COALESCE(?, '') || '%'
    OR action LIKE '%' || COALESCE(?, '') || '%'
    OR resource_id LIKE '%' || COALESCE(?, '') || '%'
    OR detail LIKE '%' || COALESCE(?, '') || '%'
  )
  AND (COALESCE(?, '') = '' OR source = COALESCE(?, ''))
  AND (COALESCE(?, '') = '' OR outcome = COALESCE(?, ''));

-- name: TrimAuditLogs :exec
DELETE FROM audit_logs WHERE id NOT IN (SELECT id FROM audit_logs ORDER BY id DESC LIMIT ?);
