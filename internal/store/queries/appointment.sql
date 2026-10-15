-- name: CreateAppointment :one
-- during_blocked est calculé en Go (domain.BlockedRange) et passé en bornes.
INSERT INTO appointment (
    practice_id, practitioner_id, patient_id, appointment_type_id,
    during, buffer_before, buffer_after, during_blocked,
    status, source, manage_token_hash, address, verified_channel
) VALUES (
    @practice_id, @practitioner_id, @patient_id, @appointment_type_id,
    tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'),
    @buffer_before, @buffer_after,
    tstzrange(@blocked_start::timestamptz, @blocked_end::timestamptz, '[)'),
    @status, @source, @manage_token_hash, @address, @verified_channel
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

-- name: ListReminderCandidates :many
-- Rappels (T023) : RDV à venir (statut booked) du cabinet qui commencent dans
-- [from_at, to_at), avec le patient pour relire contacts et opt-out au dernier moment.
-- Index appointment_practice_start_idx (practice_id, lower(during), id). Pagination par clé.
SELECT a.id, a.practice_id, a.practitioner_id, a.appointment_type_id, a.care_plan_id,
       lower(a.during)::timestamptz AS starts_at, a.address, a.verified_channel, a.updated_at,
       p.first_name, p.phone, p.email, p.sms_opt_in, p.email_opt_in,
       EXISTS (SELECT 1 FROM outbox_message o WHERE o.appointment_id = a.id
               AND o.dedup_key LIKE 'reminder-24h:' || a.id::text || ':%:' || extract(epoch FROM lower(a.during))::bigint::text) AS day_queued,
       EXISTS (SELECT 1 FROM outbox_message o WHERE o.appointment_id = a.id
               AND o.dedup_key LIKE 'reminder-2h:' || a.id::text || ':%:' || extract(epoch FROM lower(a.during))::bigint::text) AS h2_queued
FROM appointment a
JOIN patient p ON p.id = a.patient_id
WHERE a.practice_id = @practice_id
  AND a.status = 'booked'
  AND lower(a.during) >= @from_at::timestamptz
  AND lower(a.during) < @to_at::timestamptz
  AND (lower(a.during), a.id) > (@after_start::timestamptz, @after_id::uuid)
ORDER BY lower(a.during), a.id
LIMIT @page_size;

-- name: NotificationVolume :one
-- Estimation des SMS (T023) : RDV du cabinet commençant dans [from_at, to_at), non annulés,
-- classés selon les contacts disponibles du patient.
SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE p.phone IS NOT NULL AND p.sms_opt_in)::bigint AS sms_capable,
    count(*) FILTER (WHERE p.phone IS NOT NULL AND p.sms_opt_in AND NOT (p.email IS NOT NULL AND p.email_opt_in))::bigint AS sms_only
FROM appointment a
JOIN patient p ON p.id = a.patient_id
WHERE a.practice_id = @practice_id
  AND a.status <> 'cancelled'
  AND lower(a.during) >= @from_at::timestamptz
  AND lower(a.during) < @to_at::timestamptz;

-- name: LockAppointment :one
-- Lecture verrouillée (dans une transaction) : sérialise l'enfilement d'un rappel avec une annulation.
SELECT * FROM appointment WHERE id = @id FOR UPDATE;
