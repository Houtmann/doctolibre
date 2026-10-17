-- name: ScrubOutboxBatch :execrows
-- Efface le contenu (payload, destinataire) des messages terminés avant @before.
-- Index outbox_message_scrub_idx ; par lots pour ne pas tenir de gros verrous.
UPDATE outbox_message SET payload = '{}', recipient = '', last_error = NULL, scrubbed_at = @now::timestamptz, updated_at = @now::timestamptz
WHERE id IN (
    SELECT m.id FROM outbox_message m
    WHERE m.practice_id = @practice_id AND m.scrubbed_at IS NULL
      AND m.status IN ('sent', 'failed', 'cancelled')
      AND COALESCE(m.sent_at, m.updated_at) < @before::timestamptz
    LIMIT @batch_size
);

-- name: DeleteAuditLogBefore :execrows
DELETE FROM audit_log WHERE practice_id = @practice_id AND at < @before::timestamptz;

-- name: DeleteOrphanAuditLogBefore :execrows
-- Lignes sans cabinet (connexion d'un compte inconnu) : durée par défaut.
DELETE FROM audit_log WHERE practice_id IS NULL AND at < @before::timestamptz;

-- name: ListAuditLog :many
SELECT l.id, l.at, l.actor, l.action, l.entity, l.entity_id, l.meta, u.email AS user_email
FROM audit_log l
LEFT JOIN app_user u ON u.id = l.user_id
WHERE l.practice_id = @practice_id
  AND (@before_id::bigint = 0 OR l.id < @before_id::bigint)
  AND (@action::text = '' OR l.action = @action::text)
ORDER BY l.id DESC
LIMIT @page_size;

-- name: SetPracticePrivacy :one
UPDATE practice SET
    retention_outbox_days = @retention_outbox_days, retention_patient_years = @retention_patient_years,
    retention_audit_days = @retention_audit_days, patient_purge_enabled = @patient_purge_enabled,
    privacy_contact = @privacy_contact, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ListInactivePatients :many
-- Patients dont la dernière activité (création, modification, dernier rendez-vous,
-- futurs compris) précède @before. Parcours par id.
SELECT p.id, p.first_name, p.last_name, act.last_at::timestamptz AS last_activity
FROM patient p
CROSS JOIN LATERAL (
    SELECT GREATEST(p.created_at, p.updated_at, COALESCE(max(lower(a.during)), p.created_at)) AS last_at
    FROM appointment a WHERE a.patient_id = p.id
) act
WHERE p.practice_id = @practice_id AND p.anonymized_at IS NULL AND p.id > @after_id::uuid
  AND act.last_at < @before::timestamptz
ORDER BY p.id
LIMIT @page_size;

-- name: CountInactivePatients :one
SELECT count(*) FROM patient p
CROSS JOIN LATERAL (
    SELECT GREATEST(p.created_at, p.updated_at, COALESCE(max(lower(a.during)), p.created_at)) AS last_at
    FROM appointment a WHERE a.patient_id = p.id
) act
WHERE p.practice_id = @practice_id AND p.anonymized_at IS NULL AND act.last_at < @before::timestamptz;

-- name: LockPatientCluster :many
-- La fiche et toutes les fiches fusionnées dedans (récursivement), verrouillées.
WITH RECURSIVE grp AS (
    SELECT r.id FROM patient r WHERE r.id = @id AND r.practice_id = @practice_id
    UNION
    SELECT p.id FROM patient p JOIN grp g ON p.merged_into = g.id WHERE p.practice_id = @practice_id
)
SELECT * FROM patient WHERE patient.id IN (SELECT grp.id FROM grp) ORDER BY patient.id FOR UPDATE;

-- name: AnonymizePatients :execrows
UPDATE patient SET
    first_name = 'Patient', last_name = 'supprimé', birth_date = NULL, phone = NULL, email = NULL,
    address = NULL, notes_admin = '', sms_opt_in = false, email_opt_in = false,
    deleted_at = COALESCE(deleted_at, @now::timestamptz), anonymized_at = @now::timestamptz, updated_at = @now::timestamptz
WHERE id = ANY(@ids::uuid[]) AND practice_id = @practice_id;

-- name: AnonymizePatientAppointments :execrows
UPDATE appointment SET address = NULL, cancel_reason = NULL, manage_token_hash = NULL, updated_at = @now::timestamptz
WHERE patient_id = ANY(@ids::uuid[]) AND practice_id = @practice_id;

-- name: AnonymizeCarePlanLabels :exec
UPDATE care_plan SET label = '', token_version = token_version + 1, token_hash = NULL, updated_at = @now::timestamptz
WHERE patient_id = ANY(@ids::uuid[]) AND practice_id = @practice_id;

-- name: ScrubOutboxForPatients :execrows
-- Messages des rendez-vous des patients, ou adressés à leurs coordonnées
-- (codes de vérification) : annulés s'ils attendent, contenu effacé.
UPDATE outbox_message SET
    status = CASE WHEN status IN ('pending', 'sending') THEN 'cancelled' ELSE status END,
    payload = '{}', recipient = '', last_error = NULL, scrubbed_at = @now::timestamptz, updated_at = @now::timestamptz
WHERE outbox_message.practice_id = @practice_id AND outbox_message.scrubbed_at IS NULL
  AND (outbox_message.appointment_id IN (SELECT a.id FROM appointment a WHERE a.patient_id = ANY(@ids::uuid[]))
       OR (cardinality(@recipients::text[]) > 0 AND lower(outbox_message.recipient) = ANY(@recipients::text[])));

-- name: CountPatientAppointmentsAll :one
SELECT count(*) FROM appointment WHERE patient_id = ANY(@ids::uuid[]) AND practice_id = @practice_id;

-- name: CountPatientFutureBooked :one
SELECT count(*) FROM appointment
WHERE patient_id = ANY(@ids::uuid[]) AND practice_id = @practice_id AND status = 'booked' AND lower(during) >= @now::timestamptz;

-- name: DeletePatientCarePlans :exec
DELETE FROM care_plan WHERE patient_id = @patient_id AND practice_id = @practice_id;

-- name: DeletePatientRow :exec
DELETE FROM patient WHERE id = @id AND practice_id = @practice_id;

-- name: ExportPatientAppointments :many
SELECT a.id, lower(a.during)::timestamptz AS starts_at, upper(a.during)::timestamptz AS ends_at,
       a.status, a.source, t.name AS type_name, a.address, a.cancel_reason, a.cancelled_at, a.created_at
FROM appointment a
JOIN appointment_type t ON t.id = a.appointment_type_id
WHERE a.patient_id = @patient_id AND a.practice_id = @practice_id
ORDER BY lower(a.during), a.id
LIMIT 5000;

-- name: ExportPatientMessages :many
SELECT m.id, m.kind, m.template, m.status, m.recipient, m.created_at, m.sent_at
FROM outbox_message m
WHERE m.practice_id = @practice_id
  AND (m.appointment_id IN (SELECT id FROM appointment WHERE patient_id = @patient_id)
       OR (cardinality(@recipients::text[]) > 0 AND lower(m.recipient) = ANY(@recipients::text[])))
ORDER BY m.created_at, m.id
LIMIT 5000;

-- name: DeletePatientWaitlist :execrows
-- Inscriptions en liste d'attente des fiches (les propositions partent en cascade).
DELETE FROM waitlist_entry WHERE practice_id = @practice_id AND patient_id = ANY(@ids::uuid[]);

-- name: ExportPatientWaitlist :many
SELECT w.id, t.name AS type_name, w.practitioner_id, pr.first_name AS practitioner_first_name, pr.last_name AS practitioner_last_name,
       w.earliest, w.latest, w.weekdays, w.time_from, w.time_to, w.status, w.created_at
FROM waitlist_entry w
JOIN appointment_type t ON t.id = w.appointment_type_id
LEFT JOIN practitioner pr ON pr.id = w.practitioner_id
WHERE w.practice_id = @practice_id AND w.patient_id = @patient_id
ORDER BY w.created_at, w.id
LIMIT 200;
