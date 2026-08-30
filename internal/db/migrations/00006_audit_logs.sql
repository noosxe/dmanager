-- +goose Up
CREATE TABLE audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actor TEXT NOT NULL DEFAULT '',
    actor_role TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'user',
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL DEFAULT 'success',
    detail TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX idx_audit_logs_action ON audit_logs(action);

-- +goose Down
DROP INDEX IF EXISTS idx_audit_logs_action;
DROP INDEX IF EXISTS idx_audit_logs_created_at;
DROP TABLE IF EXISTS audit_logs;
