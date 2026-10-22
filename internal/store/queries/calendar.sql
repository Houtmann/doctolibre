-- name: ListCalendarAppointments :many
-- Agenda de plusieurs praticiens d'un cabinet : RDV non annulés dont la zone
-- bloquée (soin + buffers) intersecte [from_at, to_at). Utilise la contrainte
-- GiST appointment_no_overlap (practitioner_id, during_blocked) WHERE status <> 'cancelled'.
SELECT a.id, a.practitioner_id, a.status, a.source, a.patient_id,
       lower(a.during)::timestamptz AS starts_at,
       upper(a.during)::timestamptz AS ends_at,
       lower(a.during_blocked)::timestamptz AS blocked_start,
       upper(a.during_blocked)::timestamptz AS blocked_end,
       p.first_name, p.last_name,
       t.name AS type_name, t.at_home AS type_at_home, t.color AS type_color
FROM appointment a
JOIN patient p ON p.id = a.patient_id
JOIN appointment_type t ON t.id = a.appointment_type_id
WHERE a.practice_id = @practice_id
  AND a.practitioner_id = ANY(@practitioner_ids::uuid[])
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
-- Autocomplétion de l'agenda : même prédicat que SearchPatientsPage (index
-- patient_search_trgm_idx), insensible à la casse et aux accents. @phone est le
-- téléphone normalisé E.164 (ou '').
SELECT * FROM patient
WHERE practice_id = @practice_id AND deleted_at IS NULL
  AND (patient_search_text(first_name, last_name, phone, email) LIKE '%' || f_unaccent(lower(@term::text)) || '%'
       OR (@phone::text <> '' AND phone = @phone::text))
ORDER BY last_name, first_name, id
LIMIT @page_size;

-- name: ListAuditLogForEntity :many
SELECT at, actor, action, meta FROM audit_log
WHERE practice_id = @practice_id AND entity = @entity AND entity_id = @entity_id
ORDER BY at, id
LIMIT @page_size;

-- name: ListDayAppointments :many
-- « Ma journée » (T072) : RDV non annulés d'un praticien dont le soin commence
-- dans [from_at, to_at). Utilise appointment_no_overlap (practitioner_id, during_blocked).
-- series_ordinal : rang de la séance dans sa série (done/booked uniquement, 0 sinon).
SELECT a.id, a.status, a.address, a.care_plan_id,
       lower(a.during)::timestamptz AS starts_at,
       upper(a.during)::timestamptz AS ends_at,
       lower(a.during_blocked)::timestamptz AS blocked_start,
       upper(a.during_blocked)::timestamptz AS blocked_end,
       p.first_name, p.last_name, p.phone,
       t.name AS type_name, t.at_home AS type_at_home,
       COALESCE(cp.sessions_prescribed, 0)::int AS series_prescribed,
       CASE WHEN a.care_plan_id IS NOT NULL AND a.status IN ('done', 'booked') THEN
           (SELECT count(*) FROM appointment s
             WHERE s.care_plan_id = a.care_plan_id AND s.status IN ('done', 'booked')
               AND (lower(s.during), s.id) <= (lower(a.during), a.id))::int
       ELSE 0 END AS series_ordinal
FROM appointment a
JOIN patient p ON p.id = a.patient_id
JOIN appointment_type t ON t.id = a.appointment_type_id
LEFT JOIN care_plan cp ON cp.id = a.care_plan_id
WHERE a.practice_id = @practice_id
  AND a.practitioner_id = @practitioner_id
  AND a.status <> 'cancelled'
  AND a.during_blocked && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
  AND lower(a.during) >= @from_at::timestamptz AND lower(a.during) < @to_at::timestamptz
ORDER BY lower(a.during), a.id;
