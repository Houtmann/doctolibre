-- name: CreateCarePlan :one
INSERT INTO care_plan (
    practice_id, patient_id, practitioner_id, appointment_type_id, label,
    sessions_prescribed, frequency_per_week, preferred_weekdays,
    preferred_time_start, preferred_time_end, start_date
) VALUES (
    @practice_id, @patient_id, @practitioner_id, @appointment_type_id, @label,
    @sessions_prescribed, @frequency_per_week, @preferred_weekdays,
    sqlc.narg('preferred_time_start'), sqlc.narg('preferred_time_end'), @start_date
)
RETURNING *;

-- name: GetCarePlan :one
SELECT * FROM care_plan WHERE id = @id AND practice_id = @practice_id;

-- name: LockCarePlan :one
SELECT * FROM care_plan WHERE id = @id AND practice_id = @practice_id FOR UPDATE;

-- name: ListCarePlansForPatient :many
SELECT cp.*, t.name AS type_name
FROM care_plan cp
JOIN appointment_type t ON t.id = cp.appointment_type_id
WHERE cp.practice_id = @practice_id AND cp.patient_id = @patient_id
ORDER BY cp.created_at DESC, cp.id
LIMIT 50;

-- name: ListCarePlanAppointments :many
-- Séances d'une série, dans l'ordre chronologique (index appointment_care_plan_idx).
SELECT id, lower(during)::timestamptz AS starts_at, status, address
FROM appointment
WHERE care_plan_id = @care_plan_id AND practice_id = @practice_id
ORDER BY lower(during), id;

-- name: ListPatientCarePlanAppointments :many
-- Séances de toutes les séries d'un patient (index appointment_patient_idx).
SELECT id, care_plan_id, lower(during)::timestamptz AS starts_at, status
FROM appointment
WHERE patient_id = @patient_id AND practice_id = @practice_id AND care_plan_id IS NOT NULL
ORDER BY lower(during), id;

-- name: AttachAppointmentToCarePlan :exec
UPDATE appointment SET care_plan_id = @care_plan_id, updated_at = now()
WHERE id = @id AND practice_id = @practice_id;

-- name: SetCarePlanStatus :one
UPDATE care_plan SET status = @status, updated_at = now()
WHERE id = @id AND practice_id = @practice_id
RETURNING *;

-- name: ReassignPatientCarePlans :execrows
UPDATE care_plan SET patient_id = @into_id, updated_at = now()
WHERE patient_id = @from_id AND practice_id = @practice_id;

-- name: SetCarePlanToken :exec
-- Lien de série (T055) : version et haché du jeton dérivé.
UPDATE care_plan SET token_version = @version, token_hash = @token_hash
WHERE id = @id AND practice_id = @practice_id;

-- name: GetCarePlanByTokenHash :one
-- Recherche par haché (index unique care_plan_token_hash_key).
SELECT * FROM care_plan WHERE token_hash = @token_hash;

-- name: RevokeCarePlanTokens :exec
-- Fusion de fiches : les liens de série du doublon cessent de fonctionner.
UPDATE care_plan SET token_version = token_version + 1, token_hash = NULL
WHERE patient_id = @patient_id AND practice_id = @practice_id;

-- name: ListCarePlanSessions :many
-- Séances d une série avec leur jeton de gestion (page publique /s/, T055).
-- Index appointment_care_plan_idx (care_plan_id, lower(during)).
SELECT * FROM appointment
WHERE care_plan_id = @care_plan_id AND practice_id = @practice_id
ORDER BY lower(during), id;
