-- name: CreateWebAuthnCredential :one
INSERT INTO webauthn_credentials (
    credential_id,
    user_id,
    public_key,
    attestation_type,
    transport,
    aaguid,
    sign_count,
    clone_warning,
    backup_eligible,
    backup_state,
    name,
    created_at,
    last_used_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetWebAuthnCredential :one
SELECT * FROM webauthn_credentials
WHERE credential_id = ? LIMIT 1;

-- name: ListWebAuthnCredentialsByUser :many
SELECT * FROM webauthn_credentials
WHERE user_id = ?
ORDER BY created_at ASC;

-- name: CountWebAuthnCredentialsByUser :one
SELECT COUNT(*) FROM webauthn_credentials
WHERE user_id = ?;

-- name: UpdateWebAuthnCredentialUsage :exec
UPDATE webauthn_credentials
SET sign_count = ?,
    last_used_at = ?,
    backup_state = ?,
    clone_warning = ?
WHERE credential_id = ?;

-- name: RenameWebAuthnCredential :execrows
UPDATE webauthn_credentials
SET name = ?
WHERE credential_id = ? AND user_id = ?;

-- name: DeleteWebAuthnCredential :execrows
DELETE FROM webauthn_credentials
WHERE credential_id = ? AND user_id = ?;

-- name: CreateWebAuthnChallenge :one
INSERT INTO webauthn_challenges (
    challenge,
    kind,
    user_id,
    expires_at,
    consumed
) VALUES (
    ?, ?, ?, ?, 0
)
RETURNING *;

-- name: GetUnconsumedWebAuthnChallenge :one
SELECT * FROM webauthn_challenges
WHERE challenge = ? AND kind = ? AND consumed = 0 AND expires_at > ?
LIMIT 1;

-- name: ConsumeWebAuthnChallenge :exec
UPDATE webauthn_challenges
SET consumed = 1
WHERE id = ?;

-- name: PurgeExpiredWebAuthnChallenges :exec
DELETE FROM webauthn_challenges
WHERE expires_at < ? OR consumed = 1;
