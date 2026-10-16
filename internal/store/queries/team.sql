-- Équipe du cabinet et invitations (T033).

-- name: ListTeamUsers :many
SELECT u.id, u.email, u.role, u.active, u.last_login_at, u.practitioner_id,
       coalesce(p.first_name, '')::text AS first_name, coalesce(p.last_name, '')::text AS last_name,
       coalesce(p.color, '')::text AS color
FROM app_user u
LEFT JOIN practitioner p ON p.id = u.practitioner_id AND p.practice_id = u.practice_id
WHERE u.practice_id = @practice_id
ORDER BY u.active DESC, u.email, u.id
LIMIT @page_size;

-- name: GetPracticeUserForUpdate :one
SELECT * FROM app_user WHERE id = @id AND practice_id = @practice_id FOR UPDATE;

-- name: CountActiveAdmins :one
SELECT count(*) FROM app_user WHERE practice_id = @practice_id AND role = 'admin' AND active;

-- name: SetUserRole :exec
UPDATE app_user SET role = @role, updated_at = now()
WHERE id = @id AND practice_id = @practice_id;

-- name: SetUserActive :exec
UPDATE app_user SET active = @active, updated_at = now()
WHERE id = @id AND practice_id = @practice_id;

-- name: SetPractitionerActive :exec
UPDATE practitioner SET active = @active, updated_at = now()
WHERE id = @id AND practice_id = @practice_id;

-- name: SetPractitionerColor :execrows
UPDATE practitioner SET color = @color, updated_at = now()
WHERE id = @id AND practice_id = @practice_id;

-- name: CreateInvitation :exec
INSERT INTO user_invitation (token_hash, practice_id, invited_by, email, role, expires_at, created_at)
VALUES (@token_hash, @practice_id, @invited_by, @email, @role, @expires_at, @created_at);

-- name: DeletePendingInvitationsByEmail :exec
DELETE FROM user_invitation WHERE practice_id = @practice_id AND email = @email AND used_at IS NULL;

-- name: ListPendingInvitations :many
SELECT token_hash, email, role, expires_at, created_at FROM user_invitation
WHERE practice_id = @practice_id AND used_at IS NULL AND expires_at > @now
ORDER BY created_at DESC
LIMIT @page_size;

-- name: GetValidInvitation :one
SELECT * FROM user_invitation
WHERE token_hash = @token_hash AND used_at IS NULL AND expires_at > @now;

-- name: ConsumeInvitation :one
UPDATE user_invitation SET used_at = @now::timestamptz
WHERE token_hash = @token_hash AND used_at IS NULL AND expires_at > @now
RETURNING *;

-- name: RevokeInvitation :execrows
DELETE FROM user_invitation
WHERE token_hash = @token_hash AND practice_id = @practice_id AND used_at IS NULL;

-- name: DeleteExpiredInvitations :execrows
DELETE FROM user_invitation WHERE expires_at <= @now;

-- name: ListAllPractitioners :many
SELECT * FROM practitioner
WHERE practice_id = @practice_id
ORDER BY active DESC, last_name, first_name, id
LIMIT @page_size;
