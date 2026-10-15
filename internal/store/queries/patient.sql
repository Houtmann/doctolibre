-- name: CreatePatient :one
INSERT INTO patient (practice_id, first_name, last_name, birth_date, phone, email, sms_opt_in, email_opt_in, notes_admin, address)
VALUES (@practice_id, @first_name, @last_name, @birth_date, @phone, @email, @sms_opt_in, @email_opt_in, @notes_admin, @address)
RETURNING *;

-- name: GetPatient :one
SELECT * FROM patient WHERE id = @id AND practice_id = @practice_id AND deleted_at IS NULL;

-- name: ListPatients :many
SELECT * FROM patient
WHERE practice_id = @practice_id AND deleted_at IS NULL
  AND (last_name, first_name, id) > (@after_last_name::text, @after_first_name::text, @after_id::uuid)
ORDER BY last_name, first_name, id
LIMIT @page_size;

-- name: UpdatePatient :one
UPDATE patient SET
    first_name = @first_name, last_name = @last_name, birth_date = @birth_date, phone = @phone,
    email = @email, sms_opt_in = @sms_opt_in, email_opt_in = @email_opt_in,
    notes_admin = @notes_admin, updated_at = now()
WHERE id = @id AND practice_id = @practice_id AND deleted_at IS NULL
RETURNING *;
