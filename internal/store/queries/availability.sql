-- name: CreateAvailabilityRule :one
INSERT INTO availability_rule (practitioner_id, weekday, start_time, end_time, valid_from, valid_until, appointment_type_ids)
VALUES (@practitioner_id, @weekday, @start_time, @end_time, @valid_from, @valid_until, @appointment_type_ids)
RETURNING *;

-- name: ListAvailabilityRules :many
SELECT * FROM availability_rule
WHERE practitioner_id = @practitioner_id
ORDER BY weekday, start_time, id;

-- name: DeleteAvailabilityRule :exec
DELETE FROM availability_rule WHERE id = @id AND practitioner_id = @practitioner_id;

-- name: CreateAvailabilityException :one
INSERT INTO availability_exception (practitioner_id, during, kind, reason)
VALUES (@practitioner_id, tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'), @kind, @reason)
RETURNING *;

-- name: ListAvailabilityExceptions :many
SELECT * FROM availability_exception
WHERE practitioner_id = @practitioner_id
  AND during && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
ORDER BY lower(during), id;

-- name: DeleteAvailabilityException :exec
DELETE FROM availability_exception WHERE id = @id AND practitioner_id = @practitioner_id;
