# T033 — Multi-praticiens et rôles

- **Phase** : 3
- **Dépend de** : T016, T021
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Un cabinet de groupe (2–10 kinés + secrétariat) partage une instance : chacun gère son agenda, le secrétariat gère tout.

## Contexte à lire
`internal/auth/`, middleware `RequireUser`, `internal/web/routes.go`, handlers admin.

## Spécification
- Matrice de permissions dans `internal/auth/policy.go` (fonction pure `Can(user, action, resource) bool`, testée en table) :
  - `admin` : tout ; `secretary` : agendas et patients de tous, pas les réglages ; `practitioner` : son agenda, patients du cabinet en lecture/écriture, ses dispos.
- `/admin/team` (admin) : inviter un membre (email avec lien d'activation 48 h, via T021), changer le rôle, désactiver.
- Agenda : sélecteur de praticien(s) ; vue « jour » multi-colonnes (une colonne par praticien).
- Page publique : liste des praticiens + option « premier disponible » pour un motif commun.
- **Toutes** les requêtes `store` filtrent par `practice_id` (vérifier l'existant ; ajouter un test qui tente l'accès cross-cabinet).

## Critères d'acceptation
- [x] Un praticien ne peut pas modifier les dispos d'un autre (test 403) : `TestPractitionerCannotEditAnotherAvailability`.
- [ ] Vue jour à 4 praticiens lisible sur 1280 px : grille CSS à 4 colonnes égales et test HTML (`TestDayViewWithFourPractitioners`) ; rendu à 1280 px non vérifié visuellement.

## Vérification
```bash
make check
```

## Hors scope
Multi-cabinets sur une instance (hébergement mutualisé) — non prévu.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet).
- **Fait** :
  - `internal/auth/policy.go` : `Can(user, action, resource)` (matrice admin / secrétariat / praticien, refus par défaut, cloisonnement par cabinet) + `policy_test.go` table-driven. Règle « accès à l'agenda d'un praticien » partagée avec la couche service via `domain.MayAccessPractitioner`.
  - Migration `00015_team.sql` : `app_user.active`, table `user_invitation` (jeton haché sha256, usage unique, 48 h) ; requêtes `team.sql` (sqlc régénéré).
  - `auth/team.go` : `Team`, `Invite` (email via outbox dans la transaction, template `user_invitation`), `RevokeInvitation`, `Invitation`/`AcceptInvitation`, `SetRole`, `SetActive` (révoque les sessions, désactive le praticien lié, protège le dernier admin, pas d'auto-désactivation). `Login`/`Authenticate`/reset mot de passe refusent les comptes désactivés. Tests : `auth/team_test.go`.
  - Web : `/admin/team` (admin) + `/admin/invitation` (public, rate-limité) ; menu selon le rôle (`View.Role`) ; `withTarget` (middleware `practitioner=<id>` contrôlé par `auth.Can`) sur horaires/absences et création de RDV.
  - Service : `Actor{Role, PractitionerID, Target}` ; `Calendar.View` multi-praticiens (vue jour : une colonne par praticien, max 7 ; semaine : un seul) ; secrétariat sur l'agenda ; Detail/Move/Cancel/SetStatus refusent (403) un RDV d'un autre praticien pour le rôle praticien ; création de RDV pour un praticien choisi.
  - Page publique : `GET /p/{practice}/premier-disponible?type=…` (redirige vers le formulaire du créneau le plus proche) + section dans `public_practice` quand ≥ 2 praticiens reçoivent en ligne ; slug `premier-disponible` réservé.
  - Tests web : `web/team_test.go` (403 dispos, vue jour 4 praticiens, accès RDV par rôle, cross-cabinet, équipe/invitation/désactivation, premier disponible) ; `notify/invitation_template_test.go`.
- **Reste à faire** : rien d'obligatoire. Vérifier visuellement la vue jour 4 colonnes à 1280 px et le menu secrétariat dans un navigateur.
- **Notes de reprise** : tests DB avec `DOCTOLIBRE_TEST_DATABASE_URL='postgres://doctolibre:doctolibre@127.0.0.1:5432/doctolibre_test?sslmode=disable'`. `schema_test.go` : la boucle de `MigrateDown` passe de 10 à 40 itérations (le nombre de migrations dépassait 10). `Actor.Role == ""` = appel interne sans restriction par praticien (tests de service existants) ; la couche web renseigne toujours le rôle. Le jeton d'invitation est en clair dans `outbox_message.payload` (comme le jeton de réinitialisation, cf. STATUS : à purger après envoi, T043).
- **Écarts avec la spécification (et pourquoi)** :
  - Les réglages du cabinet (`/admin/settings`, motifs, notifications) restent accessibles aux praticiens comme avant (`ActionPracticeConfig` = admin + praticien) : la page mélange profil personnel et réglages du cabinet, la scinder dépasse le périmètre.
  - Séries de séances (T030) : inchangées (admin + praticien, pas de secrétariat).
  - Secrétariat sans praticien sélectionné dans le formulaire de RDV : un praticien doit être choisi (sélecteur affiché).
  - « Désactiver » un membre désactive aussi son profil praticien (plus de réservation en ligne), sans toucher à ses rendez-vous existants.
