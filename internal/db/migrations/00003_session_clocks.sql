-- +goose Up
ALTER TABLE sessions ADD COLUMN last_seen_at DATETIME NOT NULL DEFAULT '1970-01-01 00:00:00';
ALTER TABLE sessions ADD COLUMN absolute_expires_at DATETIME NOT NULL DEFAULT '1970-01-01 00:00:00';
UPDATE sessions SET absolute_expires_at = expires_at, last_seen_at = created_at;

CREATE INDEX idx_sessions_last_seen_at ON sessions(last_seen_at);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_last_seen_at;
ALTER TABLE sessions DROP COLUMN absolute_expires_at;
ALTER TABLE sessions DROP COLUMN last_seen_at;
