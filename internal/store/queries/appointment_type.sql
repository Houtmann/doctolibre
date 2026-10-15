-- name: CreateAppointmentType :one
INSERT INTO appointment_type (practice_id, name, duration, buffer_before, buffer_after, online_bookable, at_home, color, position)
VALUES (@practice_id, @name, @duration, @buffer_before, @buffer_after, @online_bookable, @at_home, @color, @position)
RETURNING *;

-- name: GetAppointmentType :one
SELECT * FROM appointment_type WHERE id = @id AND practice_id = @practice_id;

-- name: ListAppointmentTypes :many
SELECT * FROM appointment_type
WHERE practice_id = @practice_id AND active
ORDER BY position, name, id
LIMIT @page_size;

-- name: UpdateAppointmentType :one
UPDATE appointment_type SET
    name = @name, duration = @duration, buffer_before = @buffer_before, buffer_after = @buffer_after,
    online_bookable = @online_bookable, at_home = @at_home, color = @color,
    position = @position, active = @active, updated_at = now()
WHERE id = @id AND practice_id = @practice_id
RETURNING *;

-- name: AddPractitionerAppointmentType :exec
INSERT INTO practitioner_appointment_type (practitioner_id, appointment_type_id)
VALUES (@practitioner_id, @appointment_type_id)
ON CONFLICT DO NOTHING;

-- name: RemovePractitionerAppointmentType :exec
DELETE FROM practitioner_appointment_type
WHERE practitioner_id = @practitioner_id AND appointment_type_id = @appointment_type_id;

-- name: ListPractitionerAppointmentTypes :many
SELECT t.* FROM appointment_type t
JOIN practitioner_appointment_type pt ON pt.appointment_type_id = t.id
WHERE pt.practitioner_id = @practitioner_id AND t.active
ORDER BY t.position, t.name, t.id;

-- name: SetAppointmentTypeTravelBuffer :exec
UPDATE appointment_type SET travel_buffer = @travel_buffer, updated_at = now()
WHERE id = @id AND practice_id = @practice_id;
