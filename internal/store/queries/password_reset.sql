-- name: CreatePasswordReset :exec
INSERT INTO password_reset (token_hash, user_id, expires_at, created_at)
VALUES (@token_hash, @user_id, @expires_at, @created_at);

-- name: DeletePasswordResetsByUser :exec
DELETE FROM password_reset WHERE user_id = @user_id;

-- name: PasswordResetIsValid :one
SELECT EXISTS (
    SELECT 1 FROM password_reset
    WHERE token_hash = @token_hash AND used_at IS NULL AND expires_at > @now
);

-- name: ConsumePasswordReset :one
UPDATE password_reset SET used_at = @now::timestamptz
WHERE token_hash = @token_hash AND used_at IS NULL AND expires_at > @now
RETURNING user_id;

-- name: DeleteExpiredPasswordResets :execrows
DELETE FROM password_reset WHERE expires_at <= @now;
