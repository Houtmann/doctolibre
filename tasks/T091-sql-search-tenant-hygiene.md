# T091 — SQL : recherche patients indexée, filtres `practice_id` manquants

- **Phase** : 5
- **Dépend de** : T017, T088
- **Taille estimée** : S (< 200)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
L'autocomplétion patient de l'agenda utilise l'index trigramme comme la recherche des fiches, et les deux requêtes qui ne filtrent pas sur le cabinet le font (défense en profondeur, conforme à AGENTS.md).

## Contexte à lire
- `internal/store/queries/calendar.sql` (`SearchPatients` ~l. 42, `ListAuditLogForEntity` ~l. 55), `internal/store/queries/patient.sql` (recherche sur `patient_search_text(...)`), `migrations/00009_patient_search.sql`
- `internal/store/queries/appointment.sql` (`LockAppointment` ~l. 94) et ses appelants : `internal/service/reminders.go` (`enqueueOne`), `internal/service/manage_token.go` (`RevokeManageToken`)
- `internal/service/calendar.go` (`Detail`, appelant de `ListAuditLogForEntity`)

## Spécification
1. **`SearchPatients`** — remplacer les 6 `ILIKE`/`LIKE` en OR par le même prédicat que `patient.sql` sur `patient_search_text(last_name, first_name, phone, email)` (avec `deleted_at IS NULL` pour l'index partiel), en gardant tri et `LIMIT`. Vérifier que les cas actuels marchent encore (prénom nom, nom prénom, téléphone, email, insensible à la casse et aux accents si `patient_search_text` les normalise). `EXPLAIN` dans le Suivi (Bitmap Index Scan sur `patient_search_trgm_idx` attendu avec quelques milliers de patients seedés).
2. **`LockAppointment`** — ajouter `AND practice_id = @practice_id` ; adapter les deux appelants (le contrôle Go de `RevokeManageToken` devient redondant : le garder ou le remplacer par `ErrNotFound` sur `pgx.ErrNoRows`).
3. **`ListAuditLogForEntity`** — ajouter `AND practice_id = @practice_id` (index `audit_log_practice_id_idx` existant).
4. `make sqlc`.

## Critères d'acceptation
- [ ] Autocomplétion de l'agenda : mêmes résultats sur les tests existants, nouveau test (prénom + nom, téléphone partiel) ; EXPLAIN dans le Suivi.
- [ ] `LockAppointment` et `ListAuditLogForEntity` filtrent par cabinet (test : autre cabinet → aucune ligne).
- [ ] `make check` vert.

## Vérification
```bash
make sqlc && make check
```

## Hors scope
Index `outbox_message (practice_id, lower(recipient))` et `appointment (patient_id, lower(during))` : seulement si l'EXPLAIN sur données réelles déjà prévu (STATUS.md, T043) le justifie. Fusion `GetSession`+`GetUser` (gain marginal). `practice_id` sur les requêtes waitlist/outbox appelées avec des IDs internes.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
