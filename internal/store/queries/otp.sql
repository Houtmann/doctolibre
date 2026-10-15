-- name: CreateOTPChallenge :one
INSERT INTO otp_challenge (id, practice_id, channel, recipient_hash, binding_hash, code_hash, expires_at, created_at)
VALUES (@id, @practice_id, @channel, @recipient_hash, @binding_hash, @code_hash, @expires_at, @created_at)
RETURNING id;

-- name: CountRecentOTPChallenges :one
-- Rate limit par destinataire (index otp_challenge_recipient_idx).
SELECT count(*) FROM otp_challenge WHERE recipient_hash = @recipient_hash AND created_at > @since;

-- name: TryOTPChallenge :one
-- Compte une tentative de façon atomique ; aucune ligne = inconnu, expiré, consommé ou trop d'essais.
UPDATE otp_challenge SET attempts = attempts + 1
WHERE id = @id AND consumed_at IS NULL AND expires_at > @now AND attempts < @max_attempts::smallint
RETURNING practice_id, binding_hash, code_hash;

-- name: ConsumeOTPChallenge :execrows
UPDATE otp_challenge SET consumed_at = @now::timestamptz
WHERE id = @id AND consumed_at IS NULL AND expires_at > @now::timestamptz;

-- name: DeleteOldOTPChallenges :exec
DELETE FROM otp_challenge WHERE expires_at < @before;
