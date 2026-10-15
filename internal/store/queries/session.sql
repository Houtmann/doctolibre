-- name: CreateSession :one
INSERT INTO session (id, user_id, expires_at, user_agent, ip)
VALUES (@id, @user_id, @expires_at, @user_agent, @ip)
RETURNING *;

-- name: GetSession :one
SELECT * FROM session WHERE id = @id AND expires_at > @now;

-- name: DeleteSession :exec
DELETE FROM session WHERE id = @id;

-- name: DeleteExpiredSessions :execrows
DELETE FROM session WHERE expires_at <= @now;
