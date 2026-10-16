# Journal des sessions

Append-only, **la plus récente en haut**. Une entrée par session de travail (humain ou agent).
Rester court : le détail technique va dans la section « Suivi » de la tâche, les décisions dans [`decisions.md`](../decisions.md).

<!-- Modèle d'entrée :

## AAAA-MM-JJ — <titre court> (<agent/modèle ou humain>)

- **Tâches** : Txxx (todo → in_progress | done), Tyyy
- **Fait** : 2–5 puces, ce qui est vrai maintenant
- **Vérification** : `make check` ✅ / ❌ (+ ce qui échoue)
- **Reste / reprise** : ce que la prochaine session doit faire en premier
- **Décisions** : liens vers les lignes ajoutées dans decisions.md, ou « aucune »
-->

## 2026-10-16 — T052, T058, T062 en parallèle (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T052, T058, T062 (todo → review), en trois worktrees ; T050 reste en review (relecture humaine en attente).
- **Fait** : jours fériés français (option cabinet, Alsace-Moselle, migration 00019, « Férié » dans l'agenda) ; deux `VALARM` (-P1D, -PT2H) dans le `.ics` patient, aucun dans le flux praticien ; tokens `--app-*` et mode sombre dans `app.css`, `docs/design-system.md`, page `/dev/styleguide` (dev seulement).
- **Revue** : T052 en revue renforcée, une correction renvoyée : une ouverture exceptionnelle partielle rouvrait tout le jour férié ; désormais férié = journée moins les ouvertures explicites. T062 vérifiée dans le navigateur (1280 px clair, 360 px sombre). Conflit `app.css` T052 × T062 résolu à la main (ajout `.cal-holiday` en fin de fichier).
- **Vérification** : `make check` ✅ sur chaque worktree puis sur main après fusion ; `make sqlc` stable.
- **Incident** : disque plein pendant T052 (cache Go des sous-agents, 19 Go dans `/tmp/go-build`), vidé avec `go clean -cache` après accord.
- **Reste / reprise** : valider T050, T052, T058, T062 ; ensuite T051 et T055 (après T050), T057 et T061 (après T062), T053, T054, T056, T059, T060.
- **Décisions** : jours fériés en `Exception{Closed}` (journée moins ouvertures) ; design system par tokens (`decisions.md`, 2026-10-16).

## 2026-10-16 — Phase 5 ouverte, T050 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T014–T046 en review → done (validées par l'humain) ; 12 tâches T050–T061 créées (phase 5 « Retours terrain ») ; T050 → review.
- **Fait** : liste de propositions produit challengée contre le code (écartés : tournées par secteur, Web Push/Wallet, re-confirmation J-2 avec libération auto, PDF, capacité N). T050 : jeton de gestion dérivé HMAC (`manage_token_version`, migration 00018), lien dans les rappels J-1 et les annulations par le cabinet, séances de série gérables par lien, bouton « Reprendre rendez-vous » sur un RDV annulé, `DOCTOLIBRE_SECRET_KEY` obligatoire partout.
- **Revue** : spec T050 corrigée avant délégation (l'annulation ne révoque pas le lien, sinon la page « reprendre RDV » serait inaccessible ; UUID tiré en Go avant l'INSERT). Diff relu en entier.
- **Vérification** : `make check` ✅, tests DB lancés sans cache.
- **Reste / reprise** : un `.env` de dev existant doit recevoir `DOCTOLIBRE_SECRET_KEY`. Le déplacement par le cabinet (`Calendar.Move`) envoie encore un message sans lien. Prochaines : T051, T055 (débloquées après validation de T050), ou T052–T058 en parallèle.
- **Décisions** : jeton dérivé HMAC et rotation de clé (`decisions.md`, 2026-10-16).

## 2026-10-16 — T031, T033, T043, T044, T046 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T031, T033, T043, T044, T046 → review — toutes les tâches du plan sont implémentées.
- **Fait** : liste d'attente (propositions à jeton, premier qui clique gagne) ; multi-praticiens (matrice de permissions, invitations, vue jour multi-colonnes, premier disponible) ; RGPD (export, suppression/anonymisation, purge quotidienne, audit, `docs/rgpd.md`) ; doc d'installation et de coûts ; observabilité (`/metrics` branché, `/admin/status`, alertes email aux admins).
- **Revue** : T043 en revue renforcée. Trois bugs d'intégration entre tâches parallèles corrigés sur main avec tests : fausse alerte « messages en échec » après chaque purge (T043 × T046) ; suppression d'un patient inscrit en liste d'attente en échec FK + jeton `offer_url` en clair + fusion/export (T031 × T043) ; lien d'invitation en clair, invitations expirées jamais purgées, alertes envoyées aux admins désactivés (T033 × T043/T046). Lien « Confidentialité » réservé à l'admin dans la navigation.
- **Vérification** : `make check` ✅ après chaque merge (`TestCalendarWeekWith150AppointmentsIsFast` a échoué une fois sous charge, passe seul). Non vérifié : rendu 360 px et vue jour 4 praticiens à 1280 px, installation réelle sur VM (T044).
- **Reste / reprise** : relecture humaine des 14 tâches en review ; passage navigateur.
- **Décisions** : 9 entrées dans `decisions.md` (2026-10-16).

## 2026-10-15 — T023 et T030 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T023, T030 → review
- **Fait** : confirmations, annulations, déplacements et rappels J-1/H-2 (email/SMS) enfilés dans la transaction métier, tâche périodique de rappels idempotente, page `/admin/settings/notifications` avec estimation SMS ; séries de séances (proposition éditable, création atomique, badge « séance n/N », remplacement d'une séance annulée).
- **Revue** : T023 a intégré T030 (une confirmation par série). Merges sans conflit. Directive ajoutée aux sous-agents : pas de build complet à chaque étape.
- **Vérification** : `make check` ✅. Non vérifié : bout en bout réservation → Mailpit + SMS dans les logs (critère T023 non coché), rendu 360 px.
- **Reste / reprise** : relecture des 9 tâches en review ; stash `t023-wip-sonnet` laissé dans la pile git (à supprimer par l'humain) ; puis T031, T033, T043, T044.
- **Décisions** : 6 entrées dans `decisions.md` (de « Séries de séances » à « Sous-agents »).

## 2026-10-15 — T015, T017, T032, T041, T042 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T015, T017, T032, T041, T042 → review (lancées sur T014/T016 encore en review, code déjà sur `main`)
- **Fait** : annulation/déplacement par le patient via `/r/{token}` ; fiches patients (recherche sans accents, fusion de doublons) ; visites à domicile (trajet dans `during_blocked`, zones, adresse) ; sauvegardes chiffrées age + restauration testée ; flux ICS praticien à jeton révocable.
- **Revue** : 2 corrections renvoyées (T015 : SQL brut remplacé par une requête sqlc `FOR UPDATE` ; T042 : test du masquage des jetons dans les logs). Migrations pré-numérotées 00009–00011, pas de collision. Conflits résolus : `ics_test.go` (flag `-update` en double), `public.go` (union T015/T032), `sqlcgen` régénéré.
- **Vérification** : `make check` ✅ après chaque merge. `TestCalendarWeekWith150AppointmentsIsFast` a échoué une fois sous forte charge (load 20, agents en parallèle), passe seul. Non vérifié : rendu 360 px, import ICS dans un vrai client, S3 réel.
- **Reste / reprise** : relecture humaine des 7 tâches en review ; puis T023 (notifications), T030, T033, T043, T044.
- **Décisions** : 6 entrées dans `decisions.md` (de « Sauvegardes » à « Domicile »).

## 2026-10-15 — T014 et T016 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T012, T021, T022, T024, T040 → done (validées par l'humain) ; T014, T016 → review
- **Fait** : réservation en ligne patient (créneaux, OTP SMS/email via l'outbox, patient retrouvé ou créé, page `/r/{token}` + .ics) ; agenda praticien jour/semaine (création, déplacement, annulation, statut, `<dialog>` htmx, liste mobile). `domain.NormalizePhone` partagé.
- **Revue** : T014 en revue renforcée — transaction de `Book` relue (OTP consommé et créneau revalidé dans la même transaction, `EXCLUDE` tranche), test de concurrence 1 succès / 1 `ErrSlotTaken`. Conflits de merge résolus par union (`serve.go`, `routes.go`, `render_test.go`, `app.css`).
- **Vérification** : `make check` ✅. Non vérifié : rendu 360 px, agenda au clavier en vrai navigateur.
- **Reste / reprise** : relecture de T014/T016, puis T015, T017, T032, T041, T042.
- **Décisions** : 7 entrées dans `decisions.md` (de « OTP de réservation » à « `store.MoveAppointment` »).

## 2026-10-15 — 5 tâches suivantes (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T012, T021, T022, T024, T040 → review
- **Fait** : paramétrage cabinet/motifs/plages/absences ; email SMTP stdlib + mot de passe oublié ; SMS (log, HTTP générique, Android, OVH) ; worker outbox dans `serve` + sous-commande `worker` + `/admin/messages` ; Dockerfile/compose/Caddy/systemd/release + `DOCTOLIBRE_TRUSTED_PROXIES`. Dispatchers réels branchés après merge.
- **Revue** : 1 correction renvoyée (T040 : `tls` vide refusé par Caddy). À la fusion : migrations renumérotées 00005–00007, conflits `config.go`/`routes.go`/`schema_test.go` résolus, doublon du switch `DOCTOLIBRE_WORKER` retiré, `gofmt` du Makefile limité hors `.claude/`.
- **Vérification** : `make check` ✅ après chaque merge (Postgres.app). Non vérifié : Docker (build, HTTPS, mémoire), Mailpit, CI.
- **Reste / reprise** : relecture humaine des 5 tâches, puis T014 et T016.
- **Décisions** : 16 entrées dans `decisions.md` (de « IP client via TrustedProxies » à « Migrations renumérotées ») ; `AGENTS.md` : `auth → service` autorisé.

## 2026-10-15 — 10 premières tâches (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T001, T002, T003, T004, T005, T010, T011, T013, T020, T045 → done ; créées : T024 (reste de T020), T046 (reste de T045)
- **Fait** : phase 0 complète ; schéma v1 + EXCLUDE buffers inclus ; moteur de créneaux ; auth praticien ; outbox + worker ; socle métriques. Exécution en parallèle dans des worktrees, merge `--no-ff` par l'orchestrateur.
- **Revue** : 2 corrections renvoyées et faites — T005 (course `CREATE EXTENSION` entre process `go test`), T020 (double envoi possible quand un lot dépasse `StuckAfter`). Vérifications Opus : `make check` après chaque merge, `-race` sur store/outbox, page d'accueil dans le navigateur (console, CSP), login/logout de bout en bout en curl.
- **Vérification** : `make check` ✅ (avec `DOCTOLIBRE_TEST_DATABASE_URL` sur Postgres.app 17). Non vérifié : Docker (`make db-up`), CI (pas de remote), Lighthouse.
- **Reste / reprise** : T012 en priorité (chemin du MVP), puis T021/T022, T024, T040. IP client derrière Caddy à traiter dans T040.
- **Décisions** : 10 entrées dans `decisions.md` (2026-10-15, de « go.mod en go 1.26.0 » à « Travail parallèle »)

## 2026-10-15 — Répartition Opus / Sonnet (Claude Opus)

- **Tâches** : toutes (champ **Exécution** ajouté)
- **Fait** : sous-agent `.claude/agents/task-implementer.md` (Sonnet) ; commande `.claude/commands/run-task.md` (Opus : choisir, déléguer, contrôler, clore) ; section « Rôles » dans `AGENTS.md` ; revue renforcée pour T010, T011, T013, T014, T020, T043.
- **Vérification** : n/a (pas de code) — l'agent et la commande ne sont pas encore testés en réel.
- **Reste / reprise** : `git init` (prérequis du contrôle par `git diff`), puis `/run-task T001`.
- **Décisions** : répartition Opus/Sonnet

## 2026-10-15 — Suivi de l'avancement multi-session (Claude Opus)

- **Tâches** : aucune (documentation)
- **Fait** : création de `docs/README.md` (index), `docs/progress/STATUS.md` (tableau de bord), ce journal ; section « Suivi » ajoutée à chaque tâche ; statuts `in_progress`, `blocked`, `review` ajoutés ; workflow de début/fin de session dans `AGENTS.md`.
- **Vérification** : n/a (pas de code)
- **Reste / reprise** : `git init`, puis T001.
- **Décisions** : aucune

## 2026-10-15 — Intégration de la review externe (Claude Opus)

- **Tâches** : T005, T010, T013, T014, T016, T020, T023, T032, T033, T040 (spécifications modifiées)
- **Fait** : contrainte d'exclusion sur `during_blocked` (buffers inclus) et `status <> 'cancelled'` ; outbox liée au RDV par `appointment_id` ; index partiels ; tests DB par schéma ; dépendances T033→T021 et T040→T010.
- **Vérification** : n/a
- **Reste / reprise** : —
- **Décisions** : 4 entrées dans `decisions.md` (2026-10-15)

## 2026-10-15 — Comparatif SMS (Claude Opus)

- **Tâches** : T022, T023, T044 (spécifications modifiées)
- **Fait** : `docs/sms.md` (OVH ≈ 0,06 € HT, Twilio ≈ 0,07 €, passerelle Android) ; réglages par défaut : confirmation par email, rappel J-1 par SMS.
- **Vérification** : n/a
- **Reste / reprise** : vérifier les prix sur le simulateur OVH avant de chiffrer.
- **Décisions** : stratégie SMS par défaut

## 2026-10-15 — Plan initial (Claude Opus)

- **Tâches** : création de T001–T045
- **Fait** : `PLAN.md`, `AGENTS.md`, `CLAUDE.md`, `docs/decisions.md`, `tasks/`.
- **Vérification** : n/a
- **Reste / reprise** : —
- **Décisions** : stack (Go + Postgres + htmx, pas de Redis, AGPL)
