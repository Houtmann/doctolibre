-- name: GetActivePractitioner :one
SELECT * FROM practitioner WHERE id = @id AND active;

-- name: ListBookableAppointmentTypes :many
-- Motifs réservables en ligne d'un cabinet.
SELECT * FROM appointment_type
WHERE practice_id = @practice_id AND active AND online_bookable
ORDER BY position, name, id
LIMIT @page_size;

-- name: FindPatientByPhone :one
SELECT * FROM patient
WHERE practice_id = @practice_id AND phone = @phone
  AND lower(first_name) = lower(@first_name) AND lower(last_name) = lower(@last_name)
  AND deleted_at IS NULL
ORDER BY created_at, id
LIMIT 1;

-- name: FindPatientByEmail :one
SELECT * FROM patient
WHERE practice_id = @practice_id AND email = @email
  AND lower(first_name) = lower(@first_name) AND lower(last_name) = lower(@last_name)
  AND deleted_at IS NULL
ORDER BY created_at, id
LIMIT 1;
