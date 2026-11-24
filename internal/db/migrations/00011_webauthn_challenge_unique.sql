-- +goose Up
-- Finish looks a challenge up by (kind, challenge). Without an index that was
-- a full table scan, and begin is unauthenticated, so the cost of every finish
-- grew with the number of rows a client could create. Challenges are random,
-- so duplicates do not occur in practice; drop any before enforcing it.
DELETE FROM webauthn_challenges
WHERE id NOT IN (SELECT MIN(id) FROM webauthn_challenges GROUP BY kind, challenge);

CREATE UNIQUE INDEX idx_webauthn_challenges_kind_challenge ON webauthn_challenges(kind, challenge);

-- +goose Down
DROP INDEX IF EXISTS idx_webauthn_challenges_kind_challenge;
