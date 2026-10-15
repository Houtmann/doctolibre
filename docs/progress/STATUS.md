# État du projet

> Tableau de bord **réécrit à la fin de chaque session** (pas d'historique ici : voir [`journal.md`](journal.md)).
> Le statut détaillé de chaque tâche est dans [`tasks/README.md`](../../tasks/README.md), seule source de vérité.

**Mis à jour le** : 2026-10-15

## Où on en est

- **Phase 0 (Fondations) terminée.**
- **Cœur RDV** : schéma, connexion, paramétrage, moteur de créneaux faits ; **réservation (T014), gestion par le patient (T015), agenda (T016), fiches patients (T017) en `review`** → MVP sans notifications complet côté code.
- **Notifications** : outbox, worker, email SMTP, SMS 4 drivers, `/admin/messages` faits ; confirmations et rappels (T023) en `review` → v0.1 complète côté code.
- **Kiné** : visites à domicile (T032) et séries de séances (T030) en `review`.
- **Exploitation** : métriques (T045) et Docker + Caddy (T040) faits ; sauvegardes (T041) et flux ICS (T042) en `review`.
- Migrations : 00001, 00002, 00004–00011 (00009 recherche patient, 00010 domicile, 00011 jeton flux ICS, 00012 notifications, 00013 care_plan).

## En cours

_Aucune tâche en cours._ 9 tâches en `review` (T014, T015, T016, T017, T023, T030, T032, T041, T042) : relecture humaine attendue ; vérification visuelle (360 px, agenda au clavier) non faite.

## Prochaine action

1. Relire et valider les 9 tâches en review (idéalement après un passage navigateur à 360 px).
2. Puis : T031 (liste d'attente), T033 (multi-praticiens), T043 (RGPD, dépend de T017), T044 (doc d'install, dépend de T041).

## Blocages et questions ouvertes pour l'humain

- Nom de l'organisation et du module Go : `doctolibre` provisoirement (image `ghcr.io/doctolibre/doctolibre` provisoire).
- Hébergement du dépôt : la CI et `release.yml` n'ont jamais tourné, faute de remote.
- Vérifications manuelles (Docker était arrêté) :
  - `docker build -f deploy/Dockerfile .` (image ≤ 30 Mo), `caddy validate`, `docker compose up` en HTTPS (`CADDY_TLS=internal`), mémoire au repos < 50 Mo, reboot (T040) ;
  - envoi réel via Mailpit (T021) ; `make db-up` (T002, T005) ; Lighthouse ≥ 95 (T004).
- Postgres.app doit être démarré, et les tests utilisent `127.0.0.1` (pas `localhost`, qui résout en `::1` et demande un mot de passe) : `DOCTOLIBRE_TEST_DATABASE_URL=postgres://hadrien@127.0.0.1:5432/doctolibre_test?sslmode=disable`.
- `TestCalendarWeekWith150AppointmentsIsFast` (seuil 100 ms) peut échouer sous forte charge machine ; passe seul.
- À vérifier à la main : import du flux ICS dans un vrai client (T042), envoi S3 et `docker compose --profile backup up` sur serveur réel (T041).
- Réservation en ligne : seul le contact du canal du code (SMS ou email) est vérifié ; T023 pourrait notifier un contact non vérifié.
- À traiter plus tard : jeton de réinitialisation en clair dans `outbox_message.payload` après envoi (T043) ; purge des `password_reset` expirés jamais appelée ; jeton de gestion en clair dans le payload des confirmations (T043).
- Stash git `t023-wip-sonnet` laissé par un sous-agent dans la pile partagée : à supprimer (`git stash list`).
- Vérification bout en bout réservation → Mailpit + SMS dans les logs non faite (T023).

## Avancement par phase

| Phase | Tâches | Faites | En review |
|---|---|---|---|
| 0 — Fondations | 5 | 5 | 0 |
| 1 — Cœur RDV | 8 | 4 | 4 |
| 2 — Notifications | 5 | 4 | 1 |
| 3 — Spécificités kiné | 4 | 0 | 2 |
| 4 — Exploitation | 7 | 2 | 2 |
