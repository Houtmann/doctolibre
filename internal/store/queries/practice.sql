-- name: CreatePractice :one
INSERT INTO practice (name, slug, address, phone, timezone)
VALUES (@name, @slug, @address, @phone, @timezone)
RETURNING *;

-- name: GetPractice :one
SELECT * FROM practice WHERE id = @id;

-- name: GetPracticeBySlug :one
SELECT * FROM practice WHERE slug = @slug;

-- name: UpdatePractice :one
UPDATE practice SET
    name = @name, address = @address, phone = @phone, timezone = @timezone,
    min_booking_notice = @min_booking_notice, min_cancel_notice = @min_cancel_notice,
    booking_horizon = @booking_horizon, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GetPracticeByName :one
SELECT * FROM practice WHERE name = @name ORDER BY created_at, id LIMIT 1;

-- name: SetPracticeHomeVisit :exec
UPDATE practice SET home_visit_online = @home_visit_online, home_visit_zones = @home_visit_zones, updated_at = now()
WHERE id = @id;

-- name: ListPracticesPage :many
-- Parcours par clé (id) pour les tâches périodiques multi-cabinets.
SELECT * FROM practice WHERE id > @after_id ORDER BY id LIMIT @page_size;

-- name: SetPracticeNotifications :exec
UPDATE practice SET
    notify_confirmation_channel = @notify_confirmation_channel,
    notify_reminder_channel = @notify_reminder_channel,
    notify_change_channel = @notify_change_channel,
    reminder_day_before = @reminder_day_before,
    reminder_day_before_hour = @reminder_day_before_hour,
    reminder_two_hours = @reminder_two_hours,
    reminder_series = @reminder_series,
    notify_signature = @notify_signature,
    updated_at = now()
WHERE id = @id;
