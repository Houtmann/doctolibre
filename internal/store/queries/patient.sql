-- name: CreatePatient :one
INSERT INTO patient (practice_id, first_name, last_name, birth_date, phone, email, sms_opt_in, email_opt_in, notes_admin, address)
VALUES (@practice_id, @first_name, @last_name, @birth_date, @phone, @email, @sms_opt_in, @email_opt_in, @notes_admin, @address)
RETURNING *;

-- name: GetPatient :one
SELECT * FROM patient WHERE id = @id AND practice_id = @practice_id AND deleted_at IS NULL;

-- name: ListPatients :many
SELECT * FROM patient
WHERE practice_id = @practice_id AND deleted_at IS NULL
  AND (last_name, first_name, id) > (@after_last_name::text, @after_first_name::text, @after_id::uuid)
ORDER BY last_name, first_name, id
LIMIT @page_size;

-- name: UpdatePatient :one
UPDATE patient SET
    first_name = @first_name, last_name = @last_name, birth_date = @birth_date, phone = @phone,
    email = @email, sms_opt_in = @sms_opt_in, email_opt_in = @email_opt_in,
    notes_admin = @notes_admin, updated_at = now()
WHERE id = @id AND practice_id = @practice_id AND deleted_at IS NULL
RETURNING *;

-- name: ListPatientsPage :many
-- Page keyset (nom, prénom, id) des patients actifs du cabinet.
SELECT * FROM patient
WHERE practice_id = @practice_id AND deleted_at IS NULL
  AND (last_name, first_name, id) > (@after_last_name::text, @after_first_name::text, @after_id::uuid)
ORDER BY last_name, first_name, id
LIMIT @page_size;

-- name: SearchPatientsPage :many
-- Recherche insensible à la casse et aux accents (index trigram
-- patient_search_trgm_idx) ; @term est mis en minuscules sans accents ici,
-- @phone est le téléphone normalisé E.164 (ou '').
SELECT * FROM patient
WHERE practice_id = @practice_id AND deleted_at IS NULL
  AND (patient_search_text(first_name, last_name, phone, email) LIKE '%' || f_unaccent(lower(@term::text)) || '%'
       OR (@phone::text <> '' AND phone = @phone::text))
  AND (last_name, first_name, id) > (@after_last_name::text, @after_first_name::text, @after_id::uuid)
ORDER BY last_name, first_name, id
LIMIT @page_size;

-- name: FindPatientDuplicates :many
-- Fiches actives partageant le téléphone ou l'email (détection à la création).
SELECT * FROM patient
WHERE practice_id = @practice_id AND deleted_at IS NULL
  AND ((@phone::text <> '' AND phone = @phone::text) OR (@email::text <> '' AND email = @email::text))
ORDER BY last_name, first_name, id
LIMIT 5;

-- name: LockPatient :one
SELECT * FROM patient WHERE id = @id AND practice_id = @practice_id AND deleted_at IS NULL FOR UPDATE;

-- name: ListPatientAppointments :many
SELECT a.id, lower(a.during)::timestamptz AS starts_at, upper(a.during)::timestamptz AS ends_at,
       a.status, a.source, t.name AS type_name
FROM appointment a
JOIN appointment_type t ON t.id = a.appointment_type_id
WHERE a.patient_id = @patient_id AND a.practice_id = @practice_id
ORDER BY lower(a.during) DESC, a.id
LIMIT 500;

-- name: ReassignPatientAppointments :execrows
UPDATE appointment SET patient_id = @into_id, updated_at = now()
WHERE patient_id = @from_id AND practice_id = @practice_id;

-- name: AbsorbPatient :exec
-- Soft-delete de la fiche absorbée par la fusion.
UPDATE patient SET deleted_at = now(), merged_into = @into_id, updated_at = now()
WHERE id = @from_id AND practice_id = @practice_id;

-- name: MergePatientPrefs :exec
-- L'opt-out le plus restrictif l'emporte ; les coordonnées manquantes sont reprises.
UPDATE patient SET
    sms_opt_in = sms_opt_in AND @sms_opt_in::boolean,
    email_opt_in = email_opt_in AND @email_opt_in::boolean,
    phone = coalesce(phone, nullif(@phone::text, '')),
    email = coalesce(email, nullif(@email::text, '')::citext),
    updated_at = now()
WHERE id = @id AND practice_id = @practice_id;

-- name: FindImportDuplicates :many
-- Import CSV : coordonnées déjà présentes parmi les fiches actives du cabinet,
-- en une requête pour tout le fichier. Index patient_practice_phone_idx et
-- patient_practice_email_idx (BitmapOr), email comparé en citext.
SELECT coalesce(phone, '')::text AS phone, coalesce(email::text, '')::text AS email FROM patient
WHERE practice_id = @practice_id AND deleted_at IS NULL
  AND (phone = ANY(@phones::text[]) OR email = ANY(@emails::citext[]));

-- name: CreatePatientsBulk :execrows
-- Import CSV : insertion en lot depuis un tableau JSON d'objets
-- {first,last,birth,phone,email,address} ; '' devient NULL ; les consentements
-- ne sont à vrai que si la case est cochée ET que la coordonnée existe.
INSERT INTO patient (practice_id, first_name, last_name, birth_date, phone, email, address, sms_opt_in, email_opt_in)
SELECT @practice_id::uuid, r.j->>'first', r.j->>'last', nullif(r.j->>'birth', '')::date,
       nullif(r.j->>'phone', ''), nullif(r.j->>'email', '')::citext, nullif(r.j->>'address', ''),
       (@opt_in::boolean AND r.j->>'phone' <> ''), (@opt_in::boolean AND r.j->>'email' <> '')
FROM jsonb_array_elements(@rows::jsonb) AS r(j);
