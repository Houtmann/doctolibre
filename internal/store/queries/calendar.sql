-- name: ListCalendarAppointments :many
-- Agenda d'un praticien : RDV non annulés dont la zone bloquée intersecte
-- [from_at, to_at) (utilise la contrainte GiST appointment_no_overlap).
SELECT a.id, a.status, a.source, a.patient_id,
       lower(a.during)::timestamptz AS starts_at,
       upper(a.during)::timestamptz AS ends_at,
       p.first_name, p.last_name,
       t.name AS type_name, t.at_home AS type_at_home
FROM appointment a
JOIN patient p ON p.id = a.patient_id
JOIN appointment_type t ON t.id = a.appointment_type_id
WHERE a.practice_id = @practice_id
  AND a.practitioner_id = @practitioner_id
  AND a.status <> 'cancelled'
  AND a.during_blocked && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
ORDER BY lower(a.during), a.id;

-- name: GetAppointmentDetail :one
SELECT a.id, a.practitioner_id, a.patient_id, a.appointment_type_id, a.status, a.source,
       a.buffer_before, a.buffer_after,
       lower(a.during)::timestamptz AS starts_at,
       upper(a.during)::timestamptz AS ends_at,
       a.cancelled_at, a.cancelled_by, a.cancel_reason, a.created_at,
       p.first_name, p.last_name, p.phone, p.email,
       t.name AS type_name, t.at_home AS type_at_home, a.address
FROM appointment a
JOIN patient p ON p.id = a.patient_id
JOIN appointment_type t ON t.id = a.appointment_type_id
WHERE a.id = @id AND a.practice_id = @practice_id;

-- name: MoveAppointment :one
UPDATE appointment SET
    during = tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'),
    during_blocked = tstzrange(@blocked_start::timestamptz, @blocked_end::timestamptz, '[)'),
    updated_at = now()
WHERE id = @id AND practice_id = @practice_id AND status = 'booked'
RETURNING *;

-- name: SearchPatients :many
-- Recherche par préfixe du nom, du prénom, du téléphone ou de l'email.
SELECT * FROM patient
WHERE practice_id = @practice_id AND deleted_at IS NULL
  AND (last_name ILIKE @prefix::text || '%'
       OR first_name ILIKE @prefix::text || '%'
       OR (first_name || ' ' || last_name) ILIKE @prefix::text || '%'
       OR (last_name || ' ' || first_name) ILIKE @prefix::text || '%'
       OR phone LIKE @prefix::text || '%'
       OR email::text ILIKE @prefix::text || '%')
ORDER BY last_name, first_name, id
LIMIT @page_size;

-- name: ListAuditLogForEntity :many
SELECT at, actor, action, meta FROM audit_log
WHERE entity = @entity AND entity_id = @entity_id
ORDER BY at, id
LIMIT @page_size;
