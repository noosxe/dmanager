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

-- name: CreateFirstUser :one
-- Inserts a user only while the users table is empty. Check and insert are one
-- statement, so concurrent first-run setups cannot both succeed; no row is
-- returned (sql.ErrNoRows) when a user already exists.
INSERT INTO users (username, password_hash, role)
SELECT ?, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM users)
RETURNING *;
