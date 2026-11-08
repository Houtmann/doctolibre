-- Statistiques de séances (T073).

-- name: StatsSessions :many
-- Agrégat des séances faites, absences et annulations dont le DÉBUT tombe dans
-- [range_start, range_end) (bornes calculées en Go dans le fuseau du cabinet).
-- performed_by : pour un RDV « done », la fiche praticien liée au compte qui a
-- pointé la séance si elle diffère du titulaire (remplaçant) ; un compte sans fiche
-- praticien (secrétariat, admin), un status_changed_by NULL (RDV antérieur à T072)
-- ou un autre statut restent attribués au titulaire.
-- practitioner_id (optionnel) : séances dont il est titulaire ou réalisateur.
-- Aucune colonne patient. Index : appointment_practice_start_idx (practice_id, lower(during)).
WITH base AS (
    SELECT a.practitioner_id,
           a.appointment_type_id,
           a.status,
           CASE WHEN a.status = 'done'
                     AND u.practitioner_id IS NOT NULL
                     AND u.practitioner_id <> a.practitioner_id
                THEN u.practitioner_id
                ELSE a.practitioner_id
           END::uuid AS performed_by
    FROM appointment a
    LEFT JOIN app_user u ON u.id = a.status_changed_by AND u.practice_id = a.practice_id
    WHERE a.practice_id = @practice_id
      AND lower(a.during) >= @range_start::timestamptz
      AND lower(a.during) < @range_end::timestamptz
      AND a.status IN ('done', 'no_show', 'cancelled')
)
SELECT b.practitioner_id,
       pt.first_name AS practitioner_first_name,
       pt.last_name  AS practitioner_last_name,
       b.performed_by,
       pp.first_name AS performer_first_name,
       pp.last_name  AS performer_last_name,
       b.appointment_type_id,
       t.name        AS type_name,
       t.at_home,
       b.status,
       count(*)::bigint AS total
FROM base b
JOIN practitioner pt ON pt.id = b.practitioner_id AND pt.practice_id = @practice_id
JOIN practitioner pp ON pp.id = b.performed_by AND pp.practice_id = @practice_id
JOIN appointment_type t ON t.id = b.appointment_type_id AND t.practice_id = @practice_id
WHERE (sqlc.narg('practitioner_id')::uuid IS NULL
       OR b.practitioner_id = sqlc.narg('practitioner_id')::uuid
       OR b.performed_by = sqlc.narg('practitioner_id')::uuid)
GROUP BY b.practitioner_id, pt.first_name, pt.last_name, b.performed_by, pp.first_name, pp.last_name,
         b.appointment_type_id, t.name, t.at_home, b.status
ORDER BY pt.last_name, pt.first_name, b.practitioner_id, (b.performed_by <> b.practitioner_id), pp.last_name, b.performed_by, t.name, b.appointment_type_id, b.status;
