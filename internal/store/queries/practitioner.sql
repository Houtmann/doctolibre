-- name: CreatePractitioner :one
INSERT INTO practitioner (practice_id, first_name, last_name, slug, title, bio, color)
VALUES (@practice_id, @first_name, @last_name, @slug, @title, @bio, @color)
RETURNING *;

-- name: GetPractitioner :one
SELECT * FROM practitioner WHERE id = @id AND practice_id = @practice_id;

-- name: GetPractitionerBySlug :one
SELECT * FROM practitioner WHERE practice_id = @practice_id AND slug = @slug;

-- name: ListPractitioners :many
SELECT * FROM practitioner
WHERE practice_id = @practice_id AND active
ORDER BY last_name, first_name, id
LIMIT @page_size;

-- name: UpdatePractitioner :one
UPDATE practitioner SET
    first_name = @first_name, last_name = @last_name, slug = @slug, title = @title,
    bio = @bio, color = @color, active = @active, updated_at = now()
WHERE id = @id AND practice_id = @practice_id
RETURNING *;
