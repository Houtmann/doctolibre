-- name: CreateUser :one
INSERT INTO app_user (practice_id, practitioner_id, email, password_hash, role)
VALUES (@practice_id, @practitioner_id, @email, @password_hash, @role)
RETURNING *;

-- name: GetUser :one
SELECT * FROM app_user WHERE id = @id;

-- name: GetUserByEmail :one
SELECT * FROM app_user WHERE email = @email;

-- name: ListUsers :many
SELECT * FROM app_user
WHERE practice_id = @practice_id
ORDER BY email, id
LIMIT @page_size;

-- name: TouchUserLogin :exec
UPDATE app_user SET last_login_at = @at, updated_at = now() WHERE id = @id;
