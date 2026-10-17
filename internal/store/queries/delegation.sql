-- Délégations d'agenda (T059). `valid` est évalué à chaque requête contre le jour
-- LOCAL du cabinet : (now AT TIME ZONE practice.timezone)::date.

-- name: ListActiveDelegatedPractitioners :many
-- Praticiens (actifs, du même cabinet) dont l'agenda est délégué à l'utilisateur aujourd'hui.
SELECT d.practitioner_id
FROM practitioner_delegation d
JOIN practice pc ON pc.id = d.practice_id
JOIN practitioner pr ON pr.id = d.practitioner_id AND pr.practice_id = d.practice_id AND pr.active
WHERE d.user_id = @user_id
  AND d.practice_id = @practice_id
  AND d.valid @> timezone(pc.timezone, @now::timestamptz)::date
ORDER BY d.practitioner_id;

-- name: CreateDelegation :one
INSERT INTO practitioner_delegation (practice_id, user_id, practitioner_id, valid, created_by, created_at)
VALUES (@practice_id, @user_id, @practitioner_id, daterange(@valid_from::date, @valid_to::date, '[]'), @created_by, @created_at)
RETURNING id;

-- name: DeleteDelegation :one
DELETE FROM practitioner_delegation WHERE id = @id AND practice_id = @practice_id
RETURNING user_id, practitioner_id;

-- name: ListDelegations :many
-- Délégations en cours ou à venir (non terminées) du cabinet.
SELECT d.id, d.user_id, d.practitioner_id, u.email,
       pr.first_name, pr.last_name,
       lower(d.valid)::date AS valid_from, (upper(d.valid) - 1)::date AS valid_to
FROM practitioner_delegation d
JOIN practice pc ON pc.id = d.practice_id
JOIN app_user u ON u.id = d.user_id AND u.practice_id = d.practice_id
JOIN practitioner pr ON pr.id = d.practitioner_id AND pr.practice_id = d.practice_id
WHERE d.practice_id = @practice_id
  AND upper(d.valid) > timezone(pc.timezone, @now::timestamptz)::date
ORDER BY lower(d.valid), d.id
LIMIT @page_size;
