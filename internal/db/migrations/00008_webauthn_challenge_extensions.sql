-- +goose Up
-- Persist the go-webauthn SessionExtensions beside the challenge: the library
-- documents this struct as what a Relying Party "must persist between the begin
-- and finish steps of a ceremony" so extension outputs can be verified against
-- what was actually requested. Rebuilt-bare SessionData (challenge only) left
-- Requested empty, rejecting spec-mandated client outputs such as credProps.
ALTER TABLE webauthn_challenges ADD COLUMN extensions BLOB NOT NULL DEFAULT NULL;

-- +goose Down
ALTER TABLE webauthn_challenges DROP COLUMN extensions;
