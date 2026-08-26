-- +goose Up
ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';

CREATE TABLE auth_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    username TEXT NOT NULL DEFAULT '',
    event TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_auth_events_created_at ON auth_events(created_at);
CREATE INDEX idx_auth_events_user_id ON auth_events(user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_auth_events_user_id;
DROP INDEX IF EXISTS idx_auth_events_created_at;
DROP TABLE IF EXISTS auth_events;
ALTER TABLE sessions DROP COLUMN user_agent;
