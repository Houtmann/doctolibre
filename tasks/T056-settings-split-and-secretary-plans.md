# T056 — Réglages : profil vs cabinet, séries au secrétariat

- **Phase** : 5
- **Dépend de** : T033, T030
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Séparer ce qu'un praticien règle pour lui-même de ce qui engage le cabinet, et laisser le secrétariat gérer les séries de séances.

## Contexte à lire
`internal/web/admin_config.go` (`configRoutes`, `settingsForm`, `settingsSubmit`, `notifyForm`), `internal/service/practice.go` (`SettingsInput` : champs cabinet `Name, Address, Phone, BookingNoticeHours, CancelNoticeHours, HorizonDays, HomeVisit*` ; champs praticien `FirstName, LastName, Title, Bio, Slug`), `internal/web/templates/pages/admin_settings.html`, `internal/auth/policy.go` (`ActionPracticeConfig`, `ActionCarePlanManage`), `internal/auth/policy_test.go`, `internal/web/care_plans.go` (routes et `require`), `internal/domain/role.go`, `docs/` (doc des rôles, p. ex. `docs/install.md`/`README`).

## Spécification
- **(a) Scission de `/admin/settings`** : « Mon profil » (prénom, nom, titre, bio, slug, couleur, flux ICS…) accessible à tout praticien sur lui-même ; « Réglages du cabinet » (nom, adresse, téléphone, délais, fenêtre de réservation, domicile, notifications, fuseau…) réservé à l'admin. Ajouter une action `ActionPracticeSettings` (ou renommer l'usage de `ActionPracticeConfig`) dans `auth.Can` si nécessaire. Répartir précisément les champs d'après le formulaire actuel et **lister la répartition dans Suivi**. Un praticien non admin qui poste un champ cabinet reçoit 403 (pas d'ignorance silencieuse). Les liens `/admin/settings` existants doivent continuer à fonctionner (redirection ou page profil).
- **(b)** Ouvrir `ActionCarePlanManage` au rôle `secretary` (aujourd'admin + praticien, même condition que `ActionPracticeConfig` dans `Can`) ; adapter les routes de `care_plans.go` (le secrétariat choisit le praticien visé, vérifier `Actor.Target`/`MayAccessPractitioner`) et retirer la mention « 403 secrétariat » des tests de `care_plans_test.go`.
- Mettre à jour les tests de matrice (`policy_test.go`) et la doc des rôles.
- Retirer les deux questions correspondantes de `STATUS.md` ne fait **pas** partie de la tâche (orchestrateur).

## Critères d'acceptation
- [x] Un praticien non admin modifie son profil, pas les réglages du cabinet (403 testé).
- [x] L'admin modifie les deux.
- [x] Le secrétariat crée, ajuste et change le statut d'une série ; autres rôles inchangés.
- [x] Matrice `auth.Can` testée pour les nouvelles combinaisons ; doc des rôles à jour.

## Vérification
```bash
make check
```

## Hors scope
Nouveaux rôles, édition des horaires par le secrétariat, mise à jour de `STATUS.md`.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-17 (sonnet).
- **Fait** :
  - Répartition des champs. **Cabinet (admin seul, `/admin/settings`, `ActionPracticeSettings`)** : `name`, `address`, `phone`, `booking_notice_hours`, `cancel_notice_hours`, `horizon_days`, `home_visit_online`, `home_visit_zones`, `close_on_holidays`, `holidays_alsace_moselle`, `no_show_block_threshold` ; plus les notifications (`/admin/settings/notifications`) et les fournisseurs email/SMS (`/admin/settings/notifications/transport/*`), désormais admin seul (avant : admin + praticien). **Profil (admin ou praticien, sur soi-même, `/admin/profile`, `ActionProfileEdit`)** : `first_name`, `last_name`, `title`, `bio`, `slug`, `color` + abonnement d'agenda ICS (chemins `/admin/settings/feed*` conservés, redirections vers le profil).
  - Service : `SettingsInput` (cabinet) et `ProfileInput` (profil) séparés ; `UpdateSettings`/`Settings`/`Notifications`/`UpdateNotifications`/`EstimateSMS` et le transport exigent `Role` admin (`requireAdmin`, `Role` vide = appel interne) ; `UpdateProfile` (secrétaire → ErrForbidden). `Settings` ne porte plus le praticien.
  - Web : `GET /admin/settings` redirige un praticien vers `/admin/profile` (303) ; `POST /admin/settings` et pages notifications/transport : 403 pour un non-admin ; `POST /admin/profile` avec un champ cabinet (même vide) : 403 explicite, rien d'écrit. Menu : « Mon profil » (admin + praticien), « Paramètres du cabinet » (admin). Lien « Régler les fournisseurs » du tableau de bord réservé à l'admin.
  - Séries : `ActionCarePlanManage` suit `MayAccessPractitioner` (admin et secrétariat : tout praticien du cabinet ; praticien : le sien et les délégués). Routes `care-plans/new|propose|create` passent par `withTarget` (paramètre `practitioner`) ; `status`/`replace` sans cible (la série porte son praticien). Le secrétariat voit un `<select name="practitioner">` ; sans choix, erreur de validation 422 « Choisissez le praticien des séances. » ; praticien d'un autre cabinet ou identifiant invalide : 404, rien d'écrit.
  - Tests : matrice `auth.Can` (nouvelles actions, secrétariat/praticien/admin/délégation), service (`TestSettingsAdminOnlyProfileForPractitioner`, `TestUpdateProfileValidation`), web (`TestSettingsPage`, `TestProfilePage`, `TestPractitionerProfileNotPracticeSettings`, `TestSecretaryProfileForbidden`, `TestCarePlanSecretaryFlow`, `TestCarePlanSecretaryCannotTargetOtherPractice`, transport 403 praticien). Doc : `docs/install.md` (tableau des rôles).
- **Reste à faire** : passage navigateur (pages `/admin/profile`, `/admin/settings`, formulaire de série côté secrétariat) non fait.
- **Notes de reprise** : aucune migration. `bin/tailwindcss` installé dans le worktree, `ui.css` inchangé (aucune nouvelle classe).
- **Écarts avec la spécification** : `ActionPracticeConfig` renommée en `ActionPracticeSettings` (admin seul) + `ActionProfileEdit`. Les motifs de rendez-vous (`/admin/appointment-types`) restent ouverts au praticien (hors périmètre). Un praticien délégué (T059) peut viser le praticien délégué pour une série via `?practitioner=` (même règle que l'agenda) ; sans paramètre son comportement est inchangé. `status`/`replace` ne vérifient pas le praticien de la série pour un praticien (comportement inchangé).
