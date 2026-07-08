-- +goose Up
CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO settings (key, value) VALUES ('gotify_url', '');
INSERT INTO settings (key, value) VALUES ('gotify_token', '');

-- +goose Down
DROP TABLE IF EXISTS settings;
