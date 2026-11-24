-- +goose Up
-- Whether the registration ceremony verified the user (PIN, biometric, or the
-- provider's unlock check). It used to be assumed true for every credential.
-- Rows that already exist cannot be checked after the fact, so they keep
-- the old assumption (1); only new registrations record the real value.
ALTER TABLE webauthn_credentials ADD COLUMN user_verified INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE webauthn_credentials DROP COLUMN user_verified;
