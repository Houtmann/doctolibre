-- Requêtes de l'administration du cabinet (T012).

-- name: ListAllAppointmentTypes :many
SELECT * FROM appointment_type
WHERE practice_id = @practice_id
ORDER BY position, name, id
LIMIT @page_size;

-- name: CountAppointmentTypes :one
SELECT count(*) FROM appointment_type WHERE practice_id = @practice_id;

-- name: SetAppointmentTypePosition :exec
UPDATE appointment_type SET position = @position, updated_at = now()
WHERE id = @id AND practice_id = @practice_id;

-- name: GetUserForUpdate :one
SELECT * FROM app_user WHERE id = @id AND practice_id = @practice_id FOR UPDATE;

-- name: SetUserPractitioner :exec
UPDATE app_user SET practitioner_id = @practitioner_id, updated_at = now()
WHERE id = @id AND practice_id = @practice_id;

-- name: ListUpcomingClosures :many
SELECT * FROM availability_exception
WHERE practitioner_id = @practitioner_id
  AND kind = 'closed'
  AND upper(during) > @after::timestamptz
ORDER BY lower(during), id
LIMIT @page_size;
