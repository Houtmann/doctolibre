# État du projet

> Tableau de bord **réécrit à la fin de chaque session** (pas d'historique ici : voir [`journal.md`](journal.md)).
> Le statut détaillé de chaque tâche est dans [`tasks/README.md`](../../tasks/README.md), seule source de vérité.

**Mis à jour le** : 2026-10-16

## Où on en est

- **Phases 0 à 4 terminées** : 29 `done`.
- **Phase 5 — Retours terrain** ouverte : 12 tâches (T050–T061) issues d'une liste de propositions challengée le 2026-10-16. Écartés : tournées par secteur (fusion de marges incompatible avec `during_blocked` stocké), Web Push / Wallet / manifest, re-confirmation J-2 avec libération auto, PDF, capacité N des ressources (v1 = capacité 1).
- **Cœur RDV** : réservation (T014), gestion par le patient (T015), agenda (T016), fiches patients (T017) `done`.
- **Notifications** : confirmations et rappels (T023) `done`.
- **Kiné** : séries (T030), liste d'attente (T031), domicile (T032), multi-praticiens et rôles (T033) `done`.
- **Exploitation** : sauvegardes (T041), flux ICS (T042), RGPD (T043), doc d'installation (T044), observabilité (T046) `done`.
- Migrations : 00001, 00002, 00004–00017 (00014 liste d'attente, 00015 équipe, 00016 RGPD, 00017 index des échecs outbox).

## En cours

T050 (jeton de gestion dérivé) en `review`.

## Prochaine action

1. Relire T050 et la passer en `done` (débloque T051 et T055) ; en parallèle possible : T052, T053, T054, T056, T057, T058.
2. Passage navigateur : rendu 360 px de toutes les pages, agenda au clavier, vue jour à 4 praticiens à 1280 px (critère T033 non coché).
3. Installation réelle sur VM neuve en suivant `docs/install.md` (critère T044 non coché).

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
- Petits manques : le déplacement d'un RDV par le cabinet envoie un message sans lien de gestion ; pas de lien vers `/admin/status` ni vers la page de réservation publique dans le tableau de bord ; les RDV créés par une série ne comptent pas dans la métrique `AppointmentsCreated`.

## Avancement par phase

| Phase | Tâches | Faites | En review |
|---|---|---|---|
| 0 — Fondations | 5 | 5 | 0 |
| 1 — Cœur RDV | 8 | 8 | 0 |
| 2 — Notifications | 5 | 5 | 0 |
| 3 — Spécificités kiné | 4 | 4 | 0 |
| 4 — Exploitation | 7 | 7 | 0 |
| 5 — Retours terrain | 12 | 0 | 1 |
