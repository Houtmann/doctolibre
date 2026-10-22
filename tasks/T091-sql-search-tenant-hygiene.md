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
1. **`SearchPatients`** — remplacer les 6 `ILIKE`/`LIKE` en OR par le même prédicat que `SearchPatientsPage` (`patient.sql`) sur `patient_search_text(first_name, last_name, phone, email)` — ordre des arguments de la fonction et de l'index, sinon l'index n'est pas utilisé — avec `deleted_at IS NULL` pour l'index partiel, en gardant tri et `LIMIT`. Le terme est passé par `f_unaccent(lower(...))` côté SQL, comme dans `SearchPatientsPage`. *(Précision orchestrateur, 2026-10-22 : la recherche passe d'un préfixe à une sous-chaîne — accepté, c'est le comportement de la recherche des fiches ; le texte indexé contient déjà prénom/nom dans les deux ordres. Le téléphone est stocké en E.164 : reprendre aussi la branche `@phone` (téléphone normalisé, égalité) de `SearchPatientsPage` si le service sait normaliser le terme, et tester un téléphone partiel sous la forme stockée.)* Vérifier que les cas actuels marchent encore (prénom nom, nom prénom, téléphone, email, insensible à la casse et aux accents si `patient_search_text` les normalise). `EXPLAIN` dans le Suivi (Bitmap Index Scan sur `patient_search_trgm_idx` attendu avec quelques milliers de patients seedés).
2. **`LockAppointment`** — ajouter `AND practice_id = @practice_id` ; adapter les deux appelants (le contrôle Go de `RevokeManageToken` devient redondant : le garder ou le remplacer par `ErrNotFound` sur `pgx.ErrNoRows`).
3. **`ListAuditLogForEntity`** — ajouter `AND practice_id = @practice_id` (index `audit_log_practice_id_idx` existant).
4. `make sqlc`.

## Critères d'acceptation
- [x] Autocomplétion de l'agenda : mêmes résultats sur les tests existants, nouveau test (prénom + nom, téléphone partiel) ; EXPLAIN dans le Suivi.
- [x] `LockAppointment` et `ListAuditLogForEntity` filtrent par cabinet (test : autre cabinet → aucune ligne).
- [ ] `make check` vert.

## Vérification
```bash
make sqlc && make check
```

## Hors scope
Index `outbox_message (practice_id, lower(recipient))` et `appointment (patient_id, lower(during))` : seulement si l'EXPLAIN sur données réelles déjà prévu (STATUS.md, T043) le justifie. Fusion `GetSession`+`GetUser` (gain marginal). `practice_id` sur les requêtes waitlist/outbox appelées avec des IDs internes.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-22 (sonnet)
- **Fait** : `SearchPatients` (calendar.sql) reprend le prédicat de `SearchPatientsPage` (params `Term`, `Phone`; le service normalise le téléphone comme `Patients.List`) ; `LockAppointment` et `ListAuditLogForEntity` filtrent par `practice_id` ; appelants adaptés (`enqueueOne` utilise `c.PracticeID`, `RevokeManageToken` ne teste plus que `pgx.ErrNoRows`, `Detail` passe le cabinet) ; `make sqlc`. Tests : `TestSearchPatientsAutocompleteForms`, `TestAuditAndLockAreScopedToPractice` (calendar_test.go).
- **EXPLAIN** (20 000 patients, un cabinet, terme `nom3a`, base scratch migrée) : `Limit > Sort (top-N) > Bitmap Heap Scan on patient > Bitmap Index Scan on patient_search_trgm_idx` (62 lignes, Execution Time 1,0 ms).
- **Reste à faire** : —
- **Notes de reprise** : pour les tests DB en local, `DOCTOLIBRE_TEST_DATABASE_URL=postgres://hadrien@127.0.0.1:5432/doctolibre_test?sslmode=disable` (le rôle `doctolibre` n'existe pas sur cette machine ; sinon les tests sont skippés).
- **Écarts avec la spécification** : aucun (recherche passée de préfixe à sous-chaîne, comme prévu).
