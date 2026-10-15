-- name: GetFeedTokenByUser :one
SELECT * FROM calendar_feed_token WHERE user_id = @user_id;

-- name: UpsertFeedToken :exec
INSERT INTO calendar_feed_token (user_id, token_hash, full_names)
VALUES (@user_id, @token_hash, @full_names)
ON CONFLICT (user_id) DO UPDATE
SET token_hash = EXCLUDED.token_hash, full_names = EXCLUDED.full_names, created_at = now();

-- name: SetFeedFullNames :execrows
UPDATE calendar_feed_token SET full_names = @full_names WHERE user_id = @user_id;

-- name: DeleteFeedToken :exec
DELETE FROM calendar_feed_token WHERE user_id = @user_id;

-- name: GetFeedOwnerByTokenHash :one
SELECT f.user_id, f.full_names, u.practice_id, u.practitioner_id
FROM calendar_feed_token f
JOIN app_user u ON u.id = f.user_id
WHERE f.token_hash = @token_hash;

-- name: ListFeedAppointments :many
-- RDV d'un praticien (annulés inclus : STATUS:CANCELLED) dont le début bloqué est
-- dans [from_at, to_at) ; utilise appointment_practitioner_blocked_idx.
SELECT a.id, a.status, a.updated_at,
       lower(a.during)::timestamptz AS starts_at,
       upper(a.during)::timestamptz AS ends_at,
       p.first_name, p.last_name,
       t.name AS type_name
FROM appointment a
JOIN patient p ON p.id = a.patient_id
JOIN appointment_type t ON t.id = a.appointment_type_id
WHERE a.practice_id = @practice_id
  AND a.practitioner_id = @practitioner_id
  AND lower(a.during_blocked) >= @from_at::timestamptz
  AND lower(a.during_blocked) < @to_at::timestamptz
ORDER BY lower(a.during), a.id;
