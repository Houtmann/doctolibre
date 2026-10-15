# État du projet

> Tableau de bord **réécrit à la fin de chaque session** (pas d'historique ici : voir [`journal.md`](journal.md)).
> Le statut détaillé de chaque tâche est dans [`tasks/README.md`](../../tasks/README.md), seule source de vérité.

**Mis à jour le** : 2026-10-15

## Où on en est

- **Phase 0 (Fondations) terminée.**
- **Cœur RDV** : schéma v1, connexion, paramétrage, moteur de créneaux faits ; **réservation en ligne patient avec OTP (T014) et agenda praticien (T016) en `review`** → le MVP sans notifications est fonctionnel côté code.
- **Notifications** : outbox, worker dans `serve`, email SMTP, SMS 4 drivers, `/admin/messages` faits ; vrais envois branchés (`buildDispatcher`).
- **Exploitation** : métriques (T045) et packaging Docker + Caddy (T040) faits.
- Migrations : 00001, 00002, 00004, 00005 (index outbox), 00006 (plages sans chevauchement), 00007 (password_reset), 00008 (otp_challenge).

## En cours

_Aucune tâche en cours._ T014 et T016 en `review` : relecture humaine attendue, et vérification visuelle (rendu 360 px du parcours patient, agenda au clavier) non faite.

## Prochaine action

1. Relire et valider T014 et T016.
2. Puis, en parallèle : T015 (gestion du RDV par le patient, dépend de T014), T017 (dépend de T016), T032 (domicile), T042 (ICS), T041 (sauvegardes).
3. T023 (contenus des notifications) une fois T015 faite.

## Blocages et questions ouvertes pour l'humain

- Nom de l'organisation et du module Go : `doctolibre` provisoirement (image `ghcr.io/doctolibre/doctolibre` provisoire).
- Hébergement du dépôt : la CI et `release.yml` n'ont jamais tourné, faute de remote.
- Vérifications manuelles (Docker était arrêté) :
  - `docker build -f deploy/Dockerfile .` (image ≤ 30 Mo), `caddy validate`, `docker compose up` en HTTPS (`CADDY_TLS=internal`), mémoire au repos < 50 Mo, reboot (T040) ;
  - envoi réel via Mailpit (T021) ; `make db-up` (T002, T005) ; Lighthouse ≥ 95 (T004).
- Postgres.app doit être démarré, et les tests utilisent `127.0.0.1` (pas `localhost`, qui résout en `::1` et demande un mot de passe) : `DOCTOLIBRE_TEST_DATABASE_URL=postgres://hadrien@127.0.0.1:5432/doctolibre_test?sslmode=disable`.
- Réservation en ligne : seul le contact du canal du code (SMS ou email) est vérifié ; T023 pourrait notifier un contact non vérifié.
- À traiter plus tard : jeton de réinitialisation en clair dans `outbox_message.payload` après envoi (T043) ; purge des `password_reset` expirés jamais appelée ; opt-out SMS figé à l'enfilement (T023 doit enfiler les rappels peu avant l'envoi).

## Avancement par phase

| Phase | Tâches | Faites | En review |
|---|---|---|---|
| 0 — Fondations | 5 | 5 | 0 |
| 1 — Cœur RDV | 8 | 4 | 2 |
| 2 — Notifications | 5 | 4 | 0 |
| 3 — Spécificités kiné | 4 | 0 | 0 |
| 4 — Exploitation | 7 | 2 | 0 |
