-- name: CreateWaitlistEntry :one
INSERT INTO waitlist_entry (practice_id, patient_id, practitioner_id, appointment_type_id, earliest, latest, weekdays,
                            time_from, time_to, verified_channel, source, created_at)
VALUES (@practice_id, @patient_id, sqlc.narg('practitioner_id'), @appointment_type_id, @earliest, @latest, @weekdays,
        sqlc.narg('time_from'), sqlc.narg('time_to'), sqlc.narg('verified_channel'), @source, @created_at)
RETURNING *;

-- name: CountActiveWaitlistForPatient :one
SELECT count(*) FROM waitlist_entry WHERE patient_id = @patient_id AND status = 'active';

-- name: ListWaitlistEntries :many
-- Écran du cabinet : les inscriptions actives d'abord, puis les plus récentes.
SELECT w.id, w.status, w.source, w.earliest, w.latest, w.weekdays, w.time_from, w.time_to, w.created_at,
       w.patient_id, p.first_name AS patient_first_name, p.last_name AS patient_last_name,
       w.appointment_type_id, t.name AS type_name,
       w.practitioner_id, pr.first_name AS practitioner_first_name, pr.last_name AS practitioner_last_name
FROM waitlist_entry w
JOIN patient p ON p.id = w.patient_id
JOIN appointment_type t ON t.id = w.appointment_type_id
LEFT JOIN practitioner pr ON pr.id = w.practitioner_id
WHERE w.practice_id = @practice_id
ORDER BY (w.status = 'active') DESC, w.created_at DESC, w.id
LIMIT @page_size;

-- name: CancelWaitlistEntry :execrows
UPDATE waitlist_entry SET status = 'cancelled'
WHERE id = @id AND practice_id = @practice_id AND status = 'active';

-- name: ExpireWaitlistEntries :execrows
-- Passe en « expired » les inscriptions dont la dernière date est passée (jour du cabinet).
UPDATE waitlist_entry w SET status = 'expired'
FROM practice pc
WHERE pc.id = w.practice_id AND w.status = 'active'
  AND w.latest < timezone(pc.timezone, @now::timestamptz)::date;

-- name: ListWaitlistCandidates :many
-- Inscriptions actives compatibles avec un créneau libéré, par ordre d'inscription.
-- day/at : date et heure murales du cabinet ; weekday : 0 = lundi. Les inscriptions
-- ayant déjà reçu une proposition pour ce créneau sont écartées. Une seule inscription
-- (la plus ancienne) par patient : jamais deux offres du même créneau au même patient.
SELECT c.* FROM (
  SELECT DISTINCT ON (w.patient_id) w.* FROM waitlist_entry w
  WHERE w.practice_id = @practice_id AND w.status = 'active'
  AND (w.practitioner_id IS NULL OR w.practitioner_id = @practitioner_id)
  AND @day::date BETWEEN w.earliest AND w.latest
  AND (cardinality(w.weekdays) = 0 OR @weekday::smallint = ANY (w.weekdays))
  AND (w.time_from IS NULL OR (@at::time >= w.time_from AND @at::time < w.time_to))
  AND w.patient_id <> @exclude_patient
  AND NOT EXISTS (SELECT 1 FROM waitlist_offer o JOIN waitlist_entry e2 ON e2.id = o.entry_id
                  WHERE e2.patient_id = w.patient_id AND o.practitioner_id = @practitioner_id AND o.slot_start = @slot_start)
  ORDER BY w.patient_id, w.created_at, w.id
) c
ORDER BY c.created_at, c.id
LIMIT @max_rows;

-- name: CreateWaitlistOffer :one
INSERT INTO waitlist_offer (entry_id, practice_id, practitioner_id, slot_start, token_hash, expires_at, created_at)
VALUES (@entry_id, @practice_id, @practitioner_id, @slot_start, @token_hash, @expires_at, @created_at)
ON CONFLICT (entry_id, practitioner_id, slot_start) DO NOTHING
RETURNING id;

-- name: GetWaitlistOfferByToken :one
SELECT * FROM waitlist_offer WHERE token_hash = @token_hash;

-- name: LockWaitlistOfferByToken :one
SELECT * FROM waitlist_offer WHERE token_hash = @token_hash FOR UPDATE;

-- name: GetWaitlistEntry :one
SELECT * FROM waitlist_entry WHERE id = @id;

-- name: LockWaitlistEntry :one
SELECT * FROM waitlist_entry WHERE id = @id FOR UPDATE;

-- name: FulfillWaitlistEntry :exec
UPDATE waitlist_entry SET status = 'fulfilled' WHERE id = @id;

-- name: MarkWaitlistOfferClaimed :exec
UPDATE waitlist_offer SET claimed_at = @now WHERE id = @id;

-- name: ReassignPatientWaitlist :execrows
UPDATE waitlist_entry SET patient_id = @into_id
WHERE patient_id = @from_id AND practice_id = @practice_id;
