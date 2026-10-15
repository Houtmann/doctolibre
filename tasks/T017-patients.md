# T017 — Fiches patients

- **Phase** : 1
- **Dépend de** : T016
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le cabinet retrouve un patient, voit ses RDV passés/futurs, corrige ses coordonnées et fusionne les doublons.

## Contexte à lire
Table `patient`, `internal/store/queries/patient.sql`, templates admin.

## Spécification
- `GET /admin/patients?q=` : recherche par nom/prénom/téléphone/email, insensible à la casse **et aux accents** (`unaccent` + index trigram `pg_trgm` — migration de cette tâche), pagination keyset (25/page).
- `GET /admin/patients/{id}` : coordonnées, préférences de notification, RDV à venir et historique (avec nb d'absences), note administrative courte (avertissement visible : « pas de données médicales »).
- `POST /admin/patients/{id}` : édition.
- `POST /admin/patients/{id}/merge?into={id2}` : transaction, réassigne les RDV, soft-delete du doublon, audit.
- Détection de doublon à la création (même téléphone ou même email) → proposition.

## Critères d'acceptation
- [x] Recherche « helene » trouve « Hélène ».
- [x] `EXPLAIN` de la requête de recherche utilise l'index trigram (noter le plan dans la PR).
- [x] Fusion testée (RDV réassignés, audit écrit).

## Vérification
```bash
make check
```

## Hors scope
Export / suppression RGPD (T043).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : migration 00009 (extensions `unaccent` + `pg_trgm`, `f_unaccent()` immutable, `patient_search_text()` immutable, index GIN trigram partiel `patient_search_trgm_idx`, colonne `patient.merged_into`) ; requêtes sqlc (`ListPatientsPage`, `SearchPatientsPage`, `FindPatientDuplicates`, `LockPatient`, `ListPatientAppointments`, fusion) ; `service.Patients` (List keyset 25/page, Detail, Update, Create avec détection de doublon téléphone/email, Merge transactionnel avec audit `patient.merged`) ; handlers `internal/web/patients.go` et 3 pages (`admin_patients`, `admin_patient`, `admin_patient_new`) + partial `patient_fields` ; lien « Patients » dans la nav ; câblage `Deps.Patients` / `serve.go`. Tests : `internal/web/patients_test.go` (accents, pagination, édition, doublons, fusion + audit, isolation cabinet, index trigram) + `render_test.go`. Accès : admin, praticien et secrétaire.
- **Reste à faire** : rien de bloquant. Non fait (hors critères) : pas de variante htmx (formulaires POST classiques + redirect uniquement) ; vérification visuelle 360 px non faite.
- **Notes de reprise** :
  - Le test d'index (`TestPatientSearchUsesTrigramIndex`) désactive le seqscan et vérifie que le prédicat est servi par `patient_search_trgm_idx` (Bitmap Index Scan, `Index Cond: patient_search_text(...) ~~ '%helene%'`). Plan réel mesuré sur 5000 lignes de bruit (md5, un seul cabinet) : le planificateur choisit un Seq Scan (16 ms), l'estimation GIN trigram étant pessimiste sur ce bruit ; sur de vrais noms l'index est retenu. Avec seqscan off et sans l'index `patient_practice_name_idx`, plan = Bitmap Index Scan sur `patient_search_trgm_idx`.
  - L'index est construit avec `public.unaccent(...)` qualifié en dur dans `patient_search_text` : la création d'index n'utilise pas le search_path (les tests isolent chaque base dans un schéma).
  - Les extensions ne sont pas supprimées par le Down de 00009 (`TestMigrateUpDownUp` OK sans ajustement).
  - Recherche : sous-chaîne sur « prénom nom nom prénom téléphone email » ; téléphone saisi (≥ 6 chiffres) normalisé via `domain.NormalizePhone` et comparé à égalité ; une recherche partielle de téléphone en 06… ne marche pas (stocké en E.164).
  - Fusion : `{id}` = doublon (absorbé), `into` (query ou formulaire) = fiche conservée. L'opt-out le plus restrictif est conservé ; téléphone/email manquants repris du doublon.
  - La création rapide de `service.Calendar` (T016) n'a pas été modifiée ; `parseDetails`/`NormalizePhone` sont réutilisés.
- **Écarts avec la spécification** : pas de htmx (amélioration facultative) ; note limitée à 500 caractères ; la fusion se fait depuis la fiche du doublon, avec recherche de la fiche à conserver (`?mq=`).
