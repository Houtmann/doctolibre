-- name: CreateAppointment :one
-- during_blocked est calculé en Go (domain.BlockedRange) et passé en bornes.
INSERT INTO appointment (
    practice_id, practitioner_id, patient_id, appointment_type_id,
    during, buffer_before, buffer_after, during_blocked,
    status, source, manage_token_hash
) VALUES (
    @practice_id, @practitioner_id, @patient_id, @appointment_type_id,
    tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'),
    @buffer_before, @buffer_after,
    tstzrange(@blocked_start::timestamptz, @blocked_end::timestamptz, '[)'),
    @status, @source, @manage_token_hash
)
RETURNING *;

-- name: GetAppointment :one
SELECT * FROM appointment WHERE id = @id AND practice_id = @practice_id;

-- name: GetAppointmentByManageToken :one
SELECT * FROM appointment WHERE manage_token_hash = @manage_token_hash;

-- name: ListAppointmentsForPractitioner :many
-- Agenda : RDV non annulés dont le soin intersecte [from_at, to_at).
SELECT * FROM appointment
WHERE practitioner_id = @practitioner_id
  AND status <> 'cancelled'
  AND during_blocked && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
ORDER BY lower(during_blocked), id;

-- name: ListAppointmentsForPatient :many
SELECT * FROM appointment
WHERE patient_id = @patient_id AND practice_id = @practice_id
ORDER BY lower(during) DESC, id
LIMIT @page_size;

-- name: SetAppointmentStatus :one
UPDATE appointment SET status = @status, updated_at = now()
WHERE id = @id AND practice_id = @practice_id
RETURNING *;

-- name: CancelAppointment :one
UPDATE appointment SET
    status = 'cancelled', cancelled_at = @cancelled_at, cancelled_by = @cancelled_by,
    cancel_reason = @cancel_reason, updated_at = now()
WHERE id = @id AND practice_id = @practice_id AND status = 'booked'
RETURNING *;

-- name: LockAppointmentByManageToken :one
-- Lecture verrouillée (dans une transaction) du RDV d'un lien patient.
SELECT * FROM appointment WHERE manage_token_hash = @manage_token_hash FOR UPDATE;
