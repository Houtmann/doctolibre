# État du projet

> Tableau de bord **réécrit à la fin de chaque session** (pas d'historique ici : voir [`journal.md`](journal.md)).
> Le statut détaillé de chaque tâche est dans [`tasks/README.md`](../../tasks/README.md), seule source de vérité.

**Mis à jour le** : 2026-10-17 (T054, T059, T072, T086 en review)

## Où on en est

- **Phases 0 à 4 terminées** : 29 `done`.
- **Phase 5 — Retours terrain** ouverte : 29 tâches (dont T068 `superseded`) (T050–T068, dont T063–T067 = design system v2 de `design/`, finalement remplacé par la migration daisyUI T080–T084 (décision du 2026-10-16 : Tailwind v4 standalone + daisyUI 5 + EventCalendar, voir `docs/decisions.md`), et T070–T073 ajoutées le 2026-10-16 : consignes par motif, auto-planification de série par le patient, « Ma journée » + pointage, statistiques pour rétrocession) ; T050–T061 issues d'une liste de propositions challengée le 2026-10-16. Écartés : tournées par secteur (fusion de marges incompatible avec `during_blocked` stocké), Web Push / Wallet / manifest, re-confirmation J-2 avec libération auto, PDF, capacité N des ressources (v1 = capacité 1).
- **Cœur RDV** : réservation (T014), gestion par le patient (T015), agenda (T016), fiches patients (T017) `done`.
- **Notifications** : confirmations et rappels (T023) `done`.
- **Kiné** : séries (T030), liste d'attente (T031), domicile (T032), multi-praticiens et rôles (T033) `done`.
- **Exploitation** : sauvegardes (T041), flux ICS (T042), RGPD (T043), doc d'installation (T044), observabilité (T046) `done`.
- Migrations : 00001, 00002, 00004–00022 (… 00019 fériés, 00020 seuil d'absences, 00021 délégations, 00022 auteur du statut, 00023 fournisseurs de notification par cabinet).

## En cours

En `review` (relecture humaine) : T050 (jeton de gestion dérivé), T052 (jours fériés), T058 (alarmes ICS), T062 (design system), **T080** (outillage Tailwind + daisyUI, layouts ; pages non migrées volontairement sommaires jusqu’à T082–T084), **T081** (parcours patient public en daisyUI, vérifié dans le navigateur à 360/1280 px, clair et sombre), **T082** (back-office en daisyUI : 14 pages admin et 5 partials ; vérifié à 375 px sombre sur toutes les pages, erreurs de champ et focus clavier ; états avec données non vus : liste d'attente remplie, absences, invitations, pagination). **T083** (agenda EventCalendar : semaine / jour / par praticien, dialogue htmx, endpoint JSON borné à 42 jours, repli sans JS ; vérifié 1280 px clair/sombre, 360 px sombre, clavier ; non vu : JS désactivé dans un navigateur, déplacer/annuler via dialogue). **T084** (pages restantes en daisyUI, Pico, CSS `dl-*`/`--app-*`, `design/` et `poc/` supprimés ; `ui.css` seule feuille ; non vu à l'écran : parcours public confirmé/annulation/déplacement/liste d'attente, états avec données, dialogue d'agenda à 360 px). **T057** (couleurs du motif et du praticien dans l'agenda, via le JSON et le CSSOM, CSP inchangée ; l'écran manquant est T085). **T085** (couleur du praticien au profil et dans l'équipe, validée, tests ; non vu au navigateur). **T053** (import CSV des patients, aperçu puis import atomique ; non vu au navigateur ; fichier > 4 Mio → 403 CSRF générique). **T054** (blocage en ligne après N absences sur 6 mois), **T059** (remplaçant, délégation bornée en jours ; ICS non concerné), **T072** (« Ma journée » + pointage ; 360 px non vu, lien d'itinéraire OSM à vérifier) — aucune des trois vue au navigateur. **T086** (fournisseurs email/SMS par cabinet, désactivables ; migration 00023 ; non vue au navigateur). T063–T067 `done`. T068 `superseded` (absorbée par T084).

## Prochaine action

1. **Migration front (daisyUI) terminée** : relire **T080** à **T084** et **T057** (en review). Relire **T085** et **T053** (passage navigateur de l'import) et vérifier au navigateur l'en-tête de colonne « Par praticien » après changement de couleur. T061 et T070–T073 (UI) en daisyUI.
2. Relire et passer en `done` T050 (débloque T051, T055), T052, T058, T062. Prêtes : T056, T060 (attention aux numéros de migration : 00020 et suivants, un seul agent à la fois sur `sqlc`). T070 dès que T058 est `done` ; T071 après T055 ; T073 après la validation de T072 et T059.
3. Passage navigateur restant (non vu en T081–T084) : parcours public confirmé / annulation / déplacement / liste d'attente, états avec données (liste d'attente, absences, invitations, pagination), dialogue d'agenda à 360 px, déplacer/annuler via le dialogue, JS désactivé. Vue jour à 4 praticiens à 1280 px (critère T033) vue en T083/T084.
4. **T086** (fournisseurs email/SMS par cabinet, désactivables) en `review` : passage navigateur à faire (page `/admin/settings/notifications/transport`, pages publiques fermées, envoi de test vers Mailpit). Prochaine migration : 00024.
6. Installation réelle sur VM neuve en suivant `docs/install.md` (critère T044 non coché).

## Blocages et questions ouvertes pour l'humain

- Nom de l'organisation et du module Go : `doctolibre` provisoirement (image `ghcr.io/doctolibre/doctolibre` provisoire, `<URL-DU-DEPOT>` dans la doc).
- Hébergement du dépôt : la CI et `release.yml` n'ont jamais tourné, faute de remote.
- T061 (affiche QR) `blocked` : dépendance `rsc.io/qr` ou encodeur QR maison ? À trancher.
- Questions de produit :
  - IP des connexions du personnel conservée dans `audit_log.meta` (documentée dans `docs/rgpd.md`) : la garder ?
  - Fusion de fiches patients ouverte aux 3 rôles : restreindre ?
  - Le motif « Séance à domicile » est `online_bookable=false` par défaut.
- Vérifications manuelles non faites :
  - Docker : `docker build` (image ≤ 30 Mo), `caddy validate`, `docker compose up` en HTTPS, mémoire < 50 Mo, reboot (T040) ; `make db-up` (T002, T005) ; Lighthouse ≥ 95 (T004).
  - Envois : Mailpit (T021), bout en bout réservation → Mailpit + SMS (T023).
  - Import du flux ICS dans un vrai client (T042), S3 et `docker compose --profile backup up` sur serveur réel (T041).
  - EXPLAIN sur données réelles (T017, T030, T043 : purge et effacement d'un patient, qui parcourt les messages du cabinet par destinataire).
  - `.env.example` : comportement au démarrage avec le SMTP d'exemple, et du service `backup` avec `DOCTOLIBRE_BACKUP_AGE_RECIPIENT` vide (T044).
- Postgres.app doit être démarré, et les tests utilisent `127.0.0.1` (pas `localhost`) : `DOCTOLIBRE_TEST_DATABASE_URL=postgres://hadrien@127.0.0.1:5432/doctolibre_test?sslmode=disable`.
- `TestCalendarWeekWith150AppointmentsIsFast` (seuil 100 ms) peut échouer sous forte charge machine ; passe seul.
- `DOCTOLIBRE_SECRET_KEY` désormais obligatoire en dev aussi : l'ajouter à son `.env` local (exemple dans `.env.example`).
- Défaut d'affichage à confirmer : contact à la ligne dans `patient_results` (partial migré en T082, pas revu à l'écran). Puce Pico et icônes `data:` sans objet depuis le retrait de Pico (T084).
- Outillage front : `make tools` (télécharge `bin/tailwindcss`, sha256 vérifié) puis `make css` après toute modif de template ; non testé sous Linux / CI réelle.
- Disque : le cache Go des sous-agents (`/tmp/go-build`) peut remplir le disque (19 Go le 2026-10-16) ; `GOCACHE=/tmp/go-build go clean -cache`.
- Petits manques : le déplacement d'un RDV par le cabinet envoie un message sans lien de gestion ; pas de lien vers `/admin/status` ni vers la page de réservation publique dans le tableau de bord ; les RDV créés par une série ne comptent pas dans la métrique `AppointmentsCreated`.

## Avancement par phase

| Phase | Tâches | Faites | En review |
|---|---|---|---|
| 0 — Fondations | 5 | 5 | 0 |
| 1 — Cœur RDV | 8 | 8 | 0 |
| 2 — Notifications | 5 | 5 | 0 |
| 3 — Spécificités kiné | 4 | 4 | 0 |
| 4 — Exploitation | 7 | 7 | 0 |
| 5 — Retours terrain | 30 (dont 1 remplacée) | 5 | 16 |
