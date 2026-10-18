# T012 — Paramétrage cabinet, motifs, disponibilités

- **Phase** : 1
- **Dépend de** : T011
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Depuis `/admin`, le praticien configure son cabinet, ses motifs de consultation, ses horaires hebdomadaires et ses absences.

## Contexte à lire
`AGENTS.md` (Front, Handlers), `internal/web/routes.go`, `internal/web/templates/layout/admin.html`, tables `practice`, `practitioner`, `appointment_type`, `availability_rule`, `availability_exception`.

## Spécification
Pages (htmx pour les formulaires inline, mais **chaque page fonctionne aussi sans JS**) :
- `/admin/settings` : nom, adresse, téléphone, délais (notice mini de réservation, d'annulation, horizon). Profil public du praticien connecté (nom, titre, bio, slug).
- `/admin/appointment-types` : liste triable (position), création/édition/désactivation. Préremplir à la création du cabinet : « Bilan initial » 45 min, « Séance » 30 min, « Séance à domicile » 30 min (at_home, non réservable en ligne par défaut).
- `/admin/availability` : grille lundi→dimanche ; ajout/suppression de plages (ex. 08:00–12:00, 14:00–19:00) ; motifs autorisés par plage (défaut tous).
- `/admin/absences` : ajout d'absences (date+heure début/fin ou journées entières), liste à venir, suppression.
- `internal/service/practice.go` : validations métier (plages non chevauchantes le même jour pour un même praticien, durée > 0, etc.). Erreurs typées → affichées dans le formulaire.

## Critères d'acceptation
- [x] Un praticien neuf peut, en < 5 min, configurer horaires + motifs.
- [x] Deux plages qui se chevauchent le même jour → erreur lisible.
- [x] Tests service (validation) + tests handler (POST valide/invalide, CSRF).

## Vérification
```bash
make check
```

## Hors scope
Calcul des créneaux (T013), multi-praticiens (T033).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet).
- **Fait** :
  - `internal/service/` créé (`practice.go`, `appointment_types.go`, `availability.go`) : paramètres cabinet + profil praticien, motifs (création, édition, activation, ordre), plages hebdo (ajout multi-jours, suppression), absences. Erreurs `domain.ErrValidation` / `domain.ValidationErrors` (nouveau, `domain/validation.go`) affichées dans les formulaires.
  - Migration `00006_availability_overlap.sql` : contrainte `EXCLUDE` anti-chevauchement des plages (même praticien, même jour, périodes de validité qui se recoupent). Le service pré-vérifie pour un message précis et traduit aussi la violation en base.
  - Requêtes sqlc dans `queries/admin_config.sql` ; `make sqlc` relancé.
  - Pages `/admin/settings`, `/admin/appointment-types`, `/admin/availability`, `/admin/absences` (`web/admin_config.go` + templates `pages/admin_*.html`, `partials/*_panel.html`) : POST + redirect 303 sans JS, htmx renvoie le panneau seul si `HX-Request` (erreurs en 200 pour que htmx les échange). Navigation ajoutée dans `layout/admin.html`.
  - Motifs par défaut (Bilan initial 45, Séance 30, Séance à domicile 30 at_home non réservable en ligne) : `service.SeedAppointmentTypes`, appelé par `auth.CreateUser` à la création d'un cabinet neuf, et rattrapage idempotent à l'ouverture de la liste pour les cabinets existants sans motif.
  - Le praticien est créé au premier accès (`service.Practice.Practitioner`) car `create-user` ne crée pas de ligne `practitioner` ; il est lié à `app_user.practitioner_id`.
  - Tests : `internal/service/practice_test.go` (validation, chevauchement, contrainte base, DST 25 h / 23 h, isolation cabinet), `internal/web/admin_config_test.go` (POST valide/invalide, CSRF, htmx, secrétaire 403, login requis).
- **Reste à faire** : rien dans le périmètre.
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) :
  - `DOCTOLIBRE_TEST_DATABASE_URL=postgres://doctolibre:doctolibre@127.0.0.1:5432/doctolibre_test?sslmode=disable make check`.
  - Dépendance de packages : `auth → service → store` (auth appelle le seed) ; `service` n'importe pas `auth` (il reçoit un `service.Actor`). `domain.Slugify` remplace le code de `auth.Slugify` (qui délègue).
  - Les durées sont saisies en minutes entières (5–480), marges 0–120.
  - Le praticien n'est PAS lié aux motifs via `practitioner_appointment_type` : `availability_rule.appointment_type_ids` (NULL = tous) suffit pour l'instant.
- **Écarts avec la spécification** (et pourquoi) :
  - `TestMigrateUpDownUp` (T010) supposait que la dernière migration est l'outbox ; rendu générique (compte des migrations appliquées avant/après) car toute nouvelle migration le cassait.
  - `TestAllTemplatesParseAndRender` fournit des données non nulles aux 4 nouvelles pages.
  - Ajout des fonctions de template `weekdayName`, `localDate`, `localTime` (dates déjà dans le fuseau du cabinet, sans reconversion vers `Config.Timezone`).
  - Pas de htmx sur `/admin/settings` (formulaire unique, POST + redirect).
