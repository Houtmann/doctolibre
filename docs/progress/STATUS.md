# État du projet

> Tableau de bord **réécrit à la fin de chaque session** (pas d'historique ici : voir [`journal.md`](journal.md)).
> Le statut détaillé de chaque tâche est dans [`tasks/README.md`](../../tasks/README.md), seule source de vérité.

**Mis à jour le** : 2026-11-19 (T100 et T101 implémentées en parallèle, en `review`)

## Où on en est

- 71 `done`, 1 `superseded` (T068), **2 `review`** : T100 (retouches UX) et T101 (empreinte du déploiement), issues de l'essai Docker du 2026-11-11. Plus aucune tâche `todo`.
- Phases 0 à 4 terminées (29 tâches). Phase 5 — Retours terrain : 42 `done` + T068 `superseded`, dont la migration front daisyUI (T080–T085), les audits UX et sécurité (T088–T099), les consignes par motif (T070), l'auto-planification de série (T071), « Ma journée » (T072), les statistiques (T073), les ressources partagées (T060), l'affiche QR (T061).
- Validation du 2026-11-11 : passage en `done` sur décision de l'humain, sans nouvelle relecture ; les vérifications manuelles listées plus bas restent non faites.
- Migrations : 00001, 00002, 00004–00029 (00027 consignes, 00028 auto-planification, 00029 reprise sans OTP). Prochaine : 00030.

## En cours

Rien.

## Prochaine action

1. Relecture humaine de T100 et T101 ; trancher les questions T101 ci-dessous. Puis définir d'autres tâches à partir des questions produit ci-dessous, des vérifications manuelles non faites et des petits manques.
2. Vérifications au navigateur jamais faites : 360 px et thème sombre pour T061/T070/T071/T098 ; aperçu d'impression de l'affiche ; parcours public confirmé / annulation / déplacement / liste d'attente ; états avec données (liste d'attente, absences, invitations, pagination) ; dialogue d'agenda à 360 px ; JS désactivé ; pages de T051, T053, T055, T056, T059, T060, T073, T085, T086, T087, T095, T096, T097 ; envoi de test vers Mailpit.
3. Installation réelle sur VM neuve en suivant `docs/install.md` (critère T044 non coché).

## Blocages et questions ouvertes pour l'humain

- **T101** : image de sauvegarde à 199 Mo (critère < 150 Mo non tenu ; `rclone` ≈ 110 Mo) : accepter et porter la cible à ~200 Mo, ou sortir `rclone` (S3 seulement) dans une tâche à part ? Cible image app de T040 (≤ 30 Mo) à porter à ~35 Mo (32,6 Mo mesurés, binaire 18,7 Mo + distroless) ? Le sous-réseau compose fixe `172.30.0.0/24` empêche deux piles sur une même machine.
- **T100** : formater aussi le téléphone dans l'aperçu d'import CSV (test existant à adapter) ? Vérification navigateur (360 px, focus du dialogue, encart « nom ») non faite.

- Organisation et dépôt : https://github.com/Houtmann/doctolibre (créé le 2026-10-18), image `ghcr.io/houtmann/doctolibre` ; module Go toujours `doctolibre`.
- CI et `release.yml` : premier push le 2026-11-01 (main à 9404f91) ; résultat non vérifié.
- `make` refonctionne (Command Line Tools réinstallés, constaté le 2026-11-19). Postgres.app était arrêté : contrôle fait sur un `postgres:17-alpine` jetable (`--shm-size=512m -c max_locks_per_transaction=512`, sinon « out of shared memory » en parallèle).
- Statistiques (T073) : secrétariat refusé par défaut ; lui ouvrir `/admin/stats` ?
- Création de série par un admin qui a sa propre fiche praticien : le formulaire n'offre pas de choix du praticien (série rattachée à sa fiche) ; voulu (T089) ?
- Affiche (T061) ouverte à tous les rôles du cabinet : la réserver à l'admin ?
- Reprise sans OTP (T098) : les annulations **par le cabinet** (dont groupées T051) sont reprenables 30 j ; restreindre à `cancelled_by = patient` ? Rediriger un rejeu vers le nouveau RDV plutôt que la page « déjà repris » ?
- Questions de produit :
  - Remplaçant (T059) : il peut aujourd'hui modifier les **horaires hebdomadaires** du titulaire (`ActionAvailabilityEdit`, voulu par `TestCanWithDelegation`) ; la spec T059 ne cite que les absences. Le garder ? (audit du 2026-10-17)
  - IP des connexions du personnel conservée dans `audit_log.meta` (documentée dans `docs/rgpd.md`) : la garder ?
  - Fusion de fiches patients ouverte aux 3 rôles : restreindre ?
  - Le motif « Séance à domicile » est `online_bookable=false` par défaut.
- Vérifications manuelles non faites :
  - Docker : **fait le 2026-11-11 en local (OrbStack, projet compose `doctolibre-test`, `DOMAIN=localhost`, `CADDY_TLS=internal`)** : build OK, HTTPS HTTP/2 via Caddy avec CSP/HSTS, healthcheck vert, redémarrage de la pile sans perte. **Écarts** traités par T101 (mémoire 34 Mio avec `GOMEMLIMIT`, sauvegarde 585 → 199 Mo, image app 32,6 Mo inchangée). Restent : vrai domaine + Let's Encrypt, reboot machine (T040).
  - Envois : **email fait le 2026-11-11** (OTP, confirmation, déplacement, annulation, nouvelle confirmation dans Mailpit, lien vers `DOCTOLIBRE_BASE_URL`) ; SMS réel non fait.
  - Sauvegarde : fichier `.age` produit et vu dans `/admin/status` ; déchiffrement + restauration dans une base vide **faits le 2026-11-19** (T101, clé age jetable) ; reste avec la vraie clé de l'exploitant.
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
| 5 — Retours terrain | 45 (dont 1 remplacée) | 42 | 2 |
