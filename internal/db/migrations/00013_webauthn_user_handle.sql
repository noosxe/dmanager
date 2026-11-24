-- +goose Up
-- Random, opaque per-user WebAuthn user handle (32 bytes, set by the server the
-- first time a handle is needed). NULL until then.
ALTER TABLE users ADD COLUMN webauthn_user_id BLOB;

-- Users that already have passkeys keep the legacy handle, the 8-byte
-- big-endian row ID: their authenticators stored it at registration and
-- return it on sign-in, so it must never change.
UPDATE users
SET webauthn_user_id = unhex(printf('%016x', id))
WHERE EXISTS (SELECT 1 FROM webauthn_credentials c WHERE c.user_id = users.id);

CREATE UNIQUE INDEX idx_users_webauthn_user_id ON users(webauthn_user_id);

-- +goose Down
DROP INDEX idx_users_webauthn_user_id;
ALTER TABLE users DROP COLUMN webauthn_user_id;
