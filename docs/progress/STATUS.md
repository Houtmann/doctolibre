# État du projet

> Tableau de bord **réécrit à la fin de chaque session** (pas d'historique ici : voir [`journal.md`](journal.md)).
> Le statut détaillé de chaque tâche est dans [`tasks/README.md`](../../tasks/README.md), seule source de vérité.

**Mis à jour le** : 2026-11-09 (T090, T061, T070, T071, T098 en review, non committées)

## Où on en est

- **Phases 0 à 4 terminées** : 29 `done`. Phase 5 : 15 `done` (dont T056 et T072 le 2026-11-08).
- **Phase 5 — Retours terrain** ouverte : 29 tâches (dont T068 `superseded`) (T050–T068, dont T063–T067 = design system v2 de `design/`, finalement remplacé par la migration daisyUI T080–T084 (décision du 2026-10-16 : Tailwind v4 standalone + daisyUI 5 + EventCalendar, voir `docs/decisions.md`), et T070–T073 ajoutées le 2026-10-16 : consignes par motif, auto-planification de série par le patient, « Ma journée » + pointage, statistiques pour rétrocession) ; T050–T061 issues d'une liste de propositions challengée le 2026-10-16. Écartés : tournées par secteur (fusion de marges incompatible avec `during_blocked` stocké), Web Push / Wallet / manifest, re-confirmation J-2 avec libération auto, PDF, capacité N des ressources (v1 = capacité 1).
- **Cœur RDV** : réservation (T014), gestion par le patient (T015), agenda (T016), fiches patients (T017) `done`.
- **Notifications** : confirmations et rappels (T023) `done`.
- **Kiné** : séries (T030), liste d'attente (T031), domicile (T032), multi-praticiens et rôles (T033) `done`.
- **Exploitation** : sauvegardes (T041), flux ICS (T042), RGPD (T043), doc d'installation (T044), observabilité (T046) `done`.
- Migrations : 00001, 00002, 00004–00022 (… 00019 fériés, 00020 seuil d'absences, 00021 délégations, 00022 auteur du statut, 00023 fournisseurs de notification par cabinet, 00024 ressources partagées).

## En cours

**Session du 2026-11-09, rien committé (à la demande de l'humain)** : **T090** (durcissement HTTP/notify), **T061** (affiche QR `/admin/poster`, `rsc.io/qr` ; aperçu d'impression et lecture du QR non faits), **T070** (consignes par motif, migration 00027), **T071** (auto-planification de série `/s/{token}/plan`, migration 00028, invitation `series_invite`), **T098** (reprise sans OTP `/r/{token}/rebook`, migration 00029) en `review`, toutes non vues au navigateur. Lancées sur des dépendances encore en `review` (choix de l'humain).

En `review` (relecture humaine) : **T073** (statistiques de séances et export CSV pour rétrocession, `/admin/stats` ; admin et praticien, secrétariat refusé ; non vue au navigateur). **T089** (séries d'un confrère → 404, RDV sans praticien → 422 sans fiche parasite, jetons de reset purgés à la désactivation ; revue renforcée faite). **T096** (« Modifier mes coordonnées » depuis l'écran du code, réservation et liste d'attente, sans OTP ni écriture ; zones desservies dans l'aide de l'adresse ; non vue au navigateur). T094 `done` le 2026-11-01 (non vue au navigateur). **T051** (annulation groupée sur absence imprévue ; non vue au navigateur) et **T055** (lien de série `/s/{token}`, migration 00025 ; non vu au navigateur) — fusionnées dans `main` (commits d35979d, 12191a4), en attente de relecture. **T095** (déplacement et annulation par le patient : RDV actuel et semaine d'ouverture, délai annoncé, accusé d'annulation, reprise avec motif présélectionné ; non vue au navigateur). **T097** (liste d'attente côté patient : OTP dédié, récapitulatif, désinscription `/wl/{token}`, migration 00026 ; non vue au navigateur, envoi Mailpit non fait). T091 et T093 `done` le 2026-10-25. T088 `done` le 2026-10-22 (corrections RDV issues de l'audit : trajet domicile au déplacement patient, garde-fou T054 dans la liste d'attente, une offre par patient, `SetStatus` protégé, lien de gestion au déplacement par le cabinet, titre du RDV annulé, refus T054 en page 403 ; vue au navigateur sauf `Claim` via `/w/{token}` ; la page de refus est adoucie par **T092**, en `review` : page « Réservation en ligne indisponible », 403 conservé, lien `tel:`, retour vers le praticien ; vue au navigateur sur l'inscription en liste d'attente). T052 (jours fériés), T058 (alarmes ICS), T062 (design system), **T080** (outillage Tailwind + daisyUI, layouts ; pages non migrées volontairement sommaires jusqu’à T082–T084), **T081** (parcours patient public en daisyUI, vérifié dans le navigateur à 360/1280 px, clair et sombre), **T082** (back-office en daisyUI : 14 pages admin et 5 partials ; vérifié à 375 px sombre sur toutes les pages, erreurs de champ et focus clavier ; états avec données non vus : liste d'attente remplie, absences, invitations, pagination). **T083** (agenda EventCalendar : semaine / jour / par praticien, dialogue htmx, endpoint JSON borné à 42 jours, repli sans JS ; vérifié 1280 px clair/sombre, 360 px sombre, clavier ; non vu : JS désactivé dans un navigateur, déplacer/annuler via dialogue). **T084** (pages restantes en daisyUI, Pico, CSS `dl-*`/`--app-*`, `design/` et `poc/` supprimés ; `ui.css` seule feuille ; non vu à l'écran : parcours public confirmé/annulation/déplacement/liste d'attente, états avec données, dialogue d'agenda à 360 px). **T057** (couleurs du motif et du praticien dans l'agenda, via le JSON et le CSSOM, CSP inchangée ; l'écran manquant est T085). **T085** (couleur du praticien au profil et dans l'équipe, validée, tests ; non vu au navigateur). **T053** (import CSV des patients, aperçu puis import atomique ; non vu au navigateur ; fichier > 4 Mio → 403 CSRF générique). **T059** (remplaçant, délégation bornée en jours ; ICS non concerné), **T072** (« Ma journée » + pointage ; 360 px non vu, lien d'itinéraire OSM à vérifier) — aucune des deux vue au navigateur. T050 et T054 `done` (passage navigateur du 2026-10-17). **T086** (fournisseurs email/SMS par cabinet, désactivables ; migration 00023 ; non vue au navigateur). **T056** (« Mon profil » séparé des réglages du cabinet, admin seul pour cabinet / notifications / fournisseurs ; séries ouvertes au secrétariat ; non vue au navigateur). **T087** (rate limiting : `httpx.Limiter` partagé par parcours, limites admin dans `RequireUser`, quota de 30 réservations/h par praticien, 429 en fragment htmx ; non vue au navigateur). **T060** (ressources partagées : trigger `appointment_sync_resource` + EXCLUDE par ressource, CRUD dans le panneau des motifs ; non vue au navigateur). T063–T067 `done`. T068 `superseded` (absorbée par T084).

## Prochaine action

0. **Committer** les 5 tâches du 2026-11-09 (un commit par tâche ; l'arbre de travail les mélange : T071 et T098 touchent `sqlcgen`, `public.go`, `notifications.go`, `booking.go`) puis relecture et passage navigateur. Plus aucune tâche `todo` : il faut valider des tâches en review.
0. **Audit UX des parcours patients du 2026-10-17** → T095, T096 et T097 en `review` (relire ; T095+T097 débloquent T098), T098 (reprise sans OTP sous 30 jours). Décisions par défaut dans `docs/decisions.md`.
1. **Migration front (daisyUI) terminée** : relire **T080** à **T084** et **T057** (en review). Relire **T085** et **T053** (passage navigateur de l'import) et vérifier au navigateur l'en-tête de colonne « Par praticien » après changement de couleur. T061 et T070–T073 (UI) en daisyUI.
2. Relire **T051** et **T055** (déjà dans `main`) ; T071 après validation de T055. Relire et passer en `done` T052, T058, T062. Relire T060 (revue renforcée faite par l'orchestrateur ; passage navigateur `/admin/appointment-types` à faire). Aucune autre tâche `todo` n'a ses dépendances `done` : il faut valider des tâches en review (T056, T058, T059, T072, T086, T087) pour débloquer la suite. T070 dès que T058 est `done` ; T071 après T055 ; T073 et T089 en `review` depuis le 2026-11-08.
3. Passage navigateur restant (non vu en T081–T084) : parcours public confirmé / annulation / déplacement / liste d'attente, états avec données (liste d'attente, absences, invitations, pagination), dialogue d'agenda à 360 px, déplacer/annuler via le dialogue, JS désactivé. Vue jour à 4 praticiens à 1280 px (critère T033) vue en T083/T084.
4. **T086** (fournisseurs email/SMS par cabinet, désactivables) en `review` : passage navigateur à faire (page `/admin/settings/notifications/transport`, pages publiques fermées, envoi de test vers Mailpit). Prochaine migration : 00030 (00029 = T098).
5. **T056** et **T087** en `review` : passage navigateur (profil / cabinet selon le rôle, formulaire de série côté secrétariat, alerte 429 htmx). Questions ouvertes : les motifs doivent-ils passer admin seul ? limiter aussi `GET /ics/{file}` et `/p/{practice}/privacy` ?
6. **Audit du 2026-10-17** (23 constats, 9 confirmés à corriger) → T088 (RDV : trajet domicile perdu au déplacement patient, garde-fou T054 contourné par la liste d'attente — prioritaire), T089 (séries d'un confrère modifiables) en `review`, T090 (durcissement HTTP/notify — après T087), T091 (SQL) `done`.
7. Installation réelle sur VM neuve en suivant `docs/install.md` (critère T044 non coché).

## Blocages et questions ouvertes pour l'humain

- Organisation et dépôt : https://github.com/Houtmann/doctolibre (créé le 2026-10-18), image `ghcr.io/houtmann/doctolibre` ; module Go toujours `doctolibre`.
- CI et `release.yml` : premier push le 2026-11-01 (main à 9404f91) ; résultat non vérifié.
- **`make` inutilisable sur la machine de dev** (2026-11-08) : les Command Line Tools Xcode ont disparu (`xcode-select -p` en erreur ; `/usr/bin/make`, le `git` système et cgo en dépendent). À réinstaller par l'humain : `xcode-select --install`. En attendant, contrôle = étapes de `make check` à la main avec `CGO_ENABLED=0` et `PATH=/opt/homebrew/bin:$PATH`.
- Statistiques (T073) : secrétariat refusé par défaut ; lui ouvrir `/admin/stats` ?
- Affiche (T061) ouverte à tous les rôles du cabinet : la réserver à l'admin ?
- Reprise sans OTP (T098) : les annulations **par le cabinet** (dont groupées T051) sont reprenables 30 j ; restreindre à `cancelled_by = patient` ? Rediriger un rejeu vers le nouveau RDV plutôt que la page « déjà repris » ?
- Questions de produit :
  - Remplaçant (T059) : il peut aujourd'hui modifier les **horaires hebdomadaires** du titulaire (`ActionAvailabilityEdit`, voulu par `TestCanWithDelegation`) ; la spec T059 ne cite que les absences. Le garder ? (audit du 2026-10-17)
  - IP des connexions du personnel conservée dans `audit_log.meta` (documentée dans `docs/rgpd.md`) : la garder ?
  - Fusion de fiches patients ouverte aux 3 rôles : restreindre ?
  - Le motif « Séance à domicile » est `online_bookable=false` par défaut.
- Vérifications manuelles non faites :
  - Docker : `docker build` (image ≤ 30 Mo), `caddy validate`, `docker compose up` en HTTPS, mémoire < 50 Mo, reboot (T040) ; `make db-up` (T002, T005) ; Lighthouse ≥ 95 (T004).
  - Envois : Mailpit (T021), bout en bout réservation → Mailpit + SMS (T023).
  - Import du flux ICS dans un vrai client (T042), S3 et `docker compose --profile backup up` sur serveur réel (T041).
  - EXPLAIN sur données réelles (T017, T030, T043 : purge et effacement d'un patient, qui parcourt les messages du cabinet par destinataire).
  - `.env.example` : comportement au démarrage avec le SMTP d'exemple, et du service `backup` avec `DOCTOLIBRE_BACKUP_AGE_RECIPIENT` vide (T044).
- Postgres.app doit être démarré, et les tests utilisent `127.0.0.1` (pas `localhost`) : `DOCTOLIBRE_TEST_DATABASE_URL=postgres://hadrien@127.0.0.1:5432/doctolibre_test?sslmode=disable` (le rôle `doctolibre` n'existe pas sur cette machine : avec une mauvaise URL les tests DB sont **sautés sans erreur** — vérifier `go test -v … | grep -c -- '--- SKIP'` = 0 au contrôle).
- `TestCalendarWeekWith150AppointmentsIsFast` (seuil 100 ms) peut échouer sous forte charge machine ; passe seul. (Tests no-show instables corrigés le 2026-11-09.)
- `DOCTOLIBRE_SECRET_KEY` désormais obligatoire en dev aussi : l'ajouter à son `.env` local (exemple dans `.env.example`).
- Défaut d'affichage à confirmer : contact à la ligne dans `patient_results` (partial migré en T082, pas revu à l'écran). Puce Pico et icônes `data:` sans objet depuis le retrait de Pico (T084).
- Outillage front : `make tools` (télécharge `bin/tailwindcss`, sha256 vérifié) puis `make css` après toute modif de template ; non testé sous Linux / CI réelle.
- Disque : le cache Go des sous-agents (`/tmp/go-build`) peut remplir le disque (19 Go le 2026-10-16) ; `GOCACHE=/tmp/go-build go clean -cache`.
- Petits manques : pas de lien vers `/admin/status` ni vers la page de réservation publique dans le tableau de bord ; les RDV créés par une série ne comptent pas dans la métrique `AppointmentsCreated`.

## Avancement par phase

| Phase | Tâches | Faites | En review |
|---|---|---|---|
| 0 — Fondations | 5 | 5 | 0 |
| 1 — Cœur RDV | 8 | 8 | 0 |
| 2 — Notifications | 5 | 5 | 0 |
| 3 — Spécificités kiné | 4 | 4 | 0 |
| 4 — Exploitation | 7 | 7 | 0 |
| 5 — Retours terrain | 43 (dont 1 remplacée) | 15 | 27 |
