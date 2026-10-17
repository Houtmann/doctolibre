-- name: CreateResource :one
INSERT INTO resource (practice_id, name) VALUES (@practice_id, @name)
RETURNING *;

-- name: GetResource :one
SELECT * FROM resource WHERE id = @id AND practice_id = @practice_id;

-- name: ListResources :many
SELECT * FROM resource
WHERE practice_id = @practice_id
ORDER BY name, id
LIMIT @page_size;

-- name: UpdateResource :one
UPDATE resource SET name = @name, active = @active
WHERE id = @id AND practice_id = @practice_id
RETURNING *;

-- name: ListTypeResources :many
-- Liaisons motif -> ressource du cabinet (panneau des motifs).
SELECT atr.appointment_type_id, atr.resource_id
FROM appointment_type_resource atr
JOIN resource r ON r.id = atr.resource_id
WHERE r.practice_id = @practice_id
LIMIT @page_size;

-- name: SetTypeResource :exec
INSERT INTO appointment_type_resource (appointment_type_id, resource_id)
VALUES (@appointment_type_id, @resource_id)
ON CONFLICT (appointment_type_id) DO UPDATE SET resource_id = EXCLUDED.resource_id;

-- name: ClearTypeResource :exec
DELETE FROM appointment_type_resource WHERE appointment_type_id = @appointment_type_id;

-- name: ListResourceBusyForType :many
-- Occupations (during_blocked) de la ressource ACTIVE exigée par le motif, qui
-- intersectent [from_at, to_at). Index : appointment_resource_no_overlap (resource_id, during_blocked).
-- appointment_id permet d'ignorer le RDV déplacé.
SELECT ar.appointment_id, ar.during_blocked
FROM appointment_type_resource atr
JOIN resource r ON r.id = atr.resource_id AND r.active AND r.practice_id = @practice_id
JOIN appointment_resource ar ON ar.resource_id = r.id AND ar.active
WHERE atr.appointment_type_id = @appointment_type_id
  AND ar.during_blocked && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)');
