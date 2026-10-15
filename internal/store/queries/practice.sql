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
