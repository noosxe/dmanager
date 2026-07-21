-- name: SaveContainer :exec
INSERT INTO containers (
    id, name, image, image_id, state, auto_update, 
    update_available, latest_image_digest, last_checked_at, last_updated_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    image = excluded.image,
    image_id = excluded.image_id,
    state = excluded.state,
    auto_update = excluded.auto_update,
    update_available = excluded.update_available,
    latest_image_digest = excluded.latest_image_digest,
    last_checked_at = excluded.last_checked_at,
    last_updated_at = excluded.last_updated_at,
    updated_at = CURRENT_TIMESTAMP;

-- name: GetContainer :one
SELECT * FROM containers WHERE id = ? LIMIT 1;

-- name: GetContainerByName :one
SELECT * FROM containers WHERE name = ? LIMIT 1;

-- name: ListContainers :many
SELECT * FROM containers ORDER BY name ASC;

-- name: UpdateContainerUpdateState :exec
UPDATE containers
SET update_available = ?, latest_image_digest = ?, last_checked_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SetContainerAutoUpdate :exec
UPDATE containers
SET auto_update = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: GetUpgradableContainers :many
SELECT * FROM containers 
WHERE auto_update = 1 AND update_available = 1;

-- name: MarkContainerUpgraded :exec
UPDATE containers
SET update_available = 0, last_updated_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpsertContainerFromEvent :exec
INSERT INTO containers (id, name, image, image_id, state)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    image = excluded.image,
    image_id = excluded.image_id,
    state = excluded.state,
    updated_at = CURRENT_TIMESTAMP;

-- name: UpdateContainerForUpgrade :execresult
UPDATE containers
SET id = ?, name = ?, image = ?, image_id = ?, state = ?,
    update_available = 0, last_updated_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteOrphanContainers :exec
DELETE FROM containers WHERE id NOT IN (sqlc.slice('active_ids'));

-- name: DeleteContainer :exec
DELETE FROM containers WHERE id = ?;
