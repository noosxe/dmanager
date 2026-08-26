-- +goose Up
CREATE TABLE webauthn_credentials (
    credential_id BLOB PRIMARY KEY,
    user_id INTEGER NOT NULL,
    public_key BLOB NOT NULL,
    attestation_type TEXT NOT NULL,
    transport TEXT NOT NULL DEFAULT '',
    aaguid BLOB NOT NULL,
    sign_count INTEGER NOT NULL DEFAULT 0,
    clone_warning INTEGER NOT NULL DEFAULT 0,
    backup_eligible INTEGER NOT NULL DEFAULT 0,
    backup_state INTEGER NOT NULL DEFAULT 0,
    name TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX idx_webauthn_credentials_user_id ON webauthn_credentials(user_id);

CREATE TABLE webauthn_challenges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    challenge BLOB NOT NULL,
    kind TEXT NOT NULL,
    user_id INTEGER,
    expires_at DATETIME NOT NULL,
    consumed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_webauthn_challenges_expires_at ON webauthn_challenges(expires_at);

-- +goose Down
DROP TABLE IF EXISTS webauthn_challenges;
DROP TABLE IF EXISTS webauthn_credentials;
