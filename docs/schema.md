# Database Schema Design Document

This document defines the SQLite schema, table architectures, constraint mechanisms, and SQLC query definitions used in the Docker Container Manager.

---

## 1. Storage Overview

The system uses **SQLite** as a local single-file database.
* **WAL Mode:** Enabled to support multiple concurrent readers and a single concurrent writer.
* **Write Connection Cap:** Writes are serialized using a write connection pool size of `1` (`MaxOpenConns = 1`).
* **Busy Timeout:** Configured to `5000ms` (`PRAGMA busy_timeout = 5000;`) to handle lock contention gracefully.
* **Foreign Key Constraints:** Enforced at startup (`PRAGMA foreign_keys = ON;`).

---

## 2. Table Specifications

### 2.1. `users` Table
Stores user accounts for authentication and role-based access control (RBAC).

| Column | Type | Constraints | Description |
| :--- | :--- | :--- | :--- |
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | Unique user identifier. |
| `username` | TEXT | UNIQUE NOT NULL | Username (case-insensitive indexing recommended). |
| `password_hash` | TEXT | NOT NULL | Bcrypt-hashed password. |
| `role` | TEXT | NOT NULL | User role: `admin` or `viewer`. |
| `created_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | Timestamp when user was created. |
| `updated_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | Timestamp when user was last updated. |

### 2.2. `sessions` Table
Tracks active user session identifiers linked to secure browser cookies.

| Column | Type | Constraints | Description |
| :--- | :--- | :--- | :--- |
| `session_id` | TEXT | PRIMARY KEY | Cryptographically secure 32-byte hex-encoded session key. |
| `user_id` | INTEGER | NOT NULL REFERENCES `users` (`id`) ON DELETE CASCADE | Reference to the associated user. |
| `expires_at` | DATETIME | NOT NULL | Session expiration date/time. |
| `created_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | Session creation timestamp. |

### 2.3. `containers` Table
Caches discovered host container properties, scheduler inspection metrics, and update states.

| Column | Type | Constraints | Description |
| :--- | :--- | :--- | :--- |
| `id` | TEXT | PRIMARY KEY | The full 64-character hex ID of the Docker container. |
| `name` | TEXT | NOT NULL | The user-friendly name of the container. |
| `image` | TEXT | NOT NULL | Container image tag (e.g. `nginx:latest`). |
| `image_id` | TEXT | NOT NULL | Local image hash ID. |
| `state` | TEXT | NOT NULL | Current container runtime state (e.g., `running`, `stopped`). |
| `auto_update` | INTEGER | NOT NULL DEFAULT 0 | Per-container opt-in for automated updates (`1` = true, `0` = false). |
| `update_available` | INTEGER | NOT NULL DEFAULT 0 | Status of image check updates (`1` = update available, `0` = up-to-date). |
| `latest_image_digest`| TEXT | NULL | Image digest found in registry during the last update check. |
| `last_checked_at` | DATETIME | NULL | Timestamp of the last check against the registry. |
| `last_updated_at` | DATETIME | NULL | Timestamp of the last successful auto-update process. |
| `created_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | Timestamp when container record was first discovered. |
| `updated_at` | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | Timestamp of the last local state change. |

---

## 3. Database Indexes & Constraints

To optimize query operations and enforce relational integrity:
* **Unique Username:** Enforced by unique constraint and automatic unique index on `users(username)`.
* **Session Lookups:** Index on `sessions(expires_at)` to allow rapid purging of expired tokens.
* **Session User Foreign Key Index:** Index on `sessions(user_id)` to speed up cascading deletion lookups.
* **Auto-Update Scheduler Scanning:** Index on `containers(auto_update, update_available)` to instantly scan for containers that are flagged for auto-deployment when a check triggers.

---

## 4. Migration Architecture (Goose Schema)

Migrations are stored in `internal/database/migrations/` as raw SQL files.

### `00001_init.sql`
```sql
-- +goose Up
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions (
    session_id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE containers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    image TEXT NOT NULL,
    image_id TEXT NOT NULL,
    state TEXT NOT NULL,
    auto_update INTEGER NOT NULL DEFAULT 0,
    update_available INTEGER NOT NULL DEFAULT 0,
    latest_image_digest TEXT NULL,
    last_checked_at DATETIME NULL,
    last_updated_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_containers_update_state ON containers(auto_update, update_available);

-- +goose Down
DROP TABLE IF EXISTS containers;
DROP INDEX IF EXISTS idx_sessions_expires_at;
DROP INDEX IF EXISTS idx_sessions_user_id;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
```

---

## 5. SQLC Query Definitions

SQLC templates are stored in `internal/database/queries/` to generate Go handlers.

### `users.sql`
```sql
-- name: GetUser :one
SELECT * FROM users WHERE id = ? LIMIT 1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = ? LIMIT 1;

-- name: CreateUser :one
INSERT INTO users (username, password_hash, role)
VALUES (?, ?, ?)
RETURNING *;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;
```

### `sessions.sql`
```sql
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
```

### `containers.sql`
```sql
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

-- name: DeleteOrphanContainers :exec
DELETE FROM containers WHERE id NOT IN (sqlc.slice('active_ids'));
```
