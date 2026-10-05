-- +goose Up
-- Optional email identity for future system-email consumers (design.md §13).
-- Nullable by design: nothing sends mail yet and existing rows stay NULL.
-- SQLite forbids a UNIQUE constraint inside ADD COLUMN, so uniqueness is a
-- separate index; SQLite treats NULLs as distinct, so the field remains
-- truly optional while guaranteeing no two users share an address once set.
ALTER TABLE users ADD COLUMN email TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- +goose Down
DROP INDEX IF EXISTS idx_users_email;
ALTER TABLE users DROP COLUMN email;
