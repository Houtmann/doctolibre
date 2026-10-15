# État du projet

> Tableau de bord **réécrit à la fin de chaque session** (pas d'historique ici : voir [`journal.md`](journal.md)).
> Le statut détaillé de chaque tâche est dans [`tasks/README.md`](../../tasks/README.md), seule source de vérité.

**Mis à jour le** : 2026-10-15

## Où on en est

- **Phase 0 (Fondations) terminée.**
- **Cœur RDV** : schéma v1 (T010), moteur de créneaux (T013), connexion praticien (T011) faits ; **paramétrage cabinet, motifs, plages et absences (T012) en `review`**.
- **Notifications** : outbox + worker (T020) fait ; **email SMTP + mot de passe oublié (T021), SMS 4 drivers (T022), worker lancé par `serve` et page `/admin/messages` (T024) en `review`**. Les vrais envois sont branchés (`buildDispatcher`) ; en prod un driver `log` donne un avertissement.
- **Exploitation** : métriques (T045) faites ; **packaging Docker + Caddy, IP client derrière proxy (T040) en `review`**.
- Migrations : 00001, 00002, 00004, 00005 (index outbox), 00006 (plages sans chevauchement), 00007 (password_reset).

## En cours

_Aucune tâche en cours._ 5 tâches en `review` attendent une relecture humaine : T012, T021, T022, T024, T040.

## Prochaine action

1. Relire et valider les 5 tâches en `review` (→ `done`).
2. Une fois T012 validée : [T014 — Réservation en ligne patient](../../tasks/T014-public-booking.md) et [T016 — Agenda praticien](../../tasks/T016-staff-calendar.md) en parallèle (chemin du MVP).
3. Une fois T040 validée : T041 (sauvegardes).

## Blocages et questions ouvertes pour l'humain

- Nom de l'organisation et du module Go : `doctolibre` provisoirement (image `ghcr.io/doctolibre/doctolibre` provisoire).
- Hébergement du dépôt : la CI et `release.yml` n'ont jamais tourné, faute de remote.
- Vérifications manuelles (Docker était arrêté) :
  - `docker build -f deploy/Dockerfile .` (image ≤ 30 Mo), `caddy validate`, `docker compose up` en HTTPS (`CADDY_TLS=internal`), mémoire au repos < 50 Mo, reboot (T040) ;
  - envoi réel via Mailpit (T021) ; `make db-up` (T002, T005) ; Lighthouse ≥ 95 (T004).
- Avant T014 : `practitioner_appointment_type` n'est pas alimentée par T012 (la restriction passe par `availability_rule.appointment_type_ids`). Décider si T014 s'en sert.
- À traiter plus tard : jeton de réinitialisation en clair dans `outbox_message.payload` après envoi (T043) ; purge des `password_reset` expirés jamais appelée ; opt-out SMS figé à l'enfilement (T023 doit enfiler les rappels peu avant l'envoi).

## Avancement par phase

| Phase | Tâches | Faites | En review |
|---|---|---|---|
| 0 — Fondations | 5 | 5 | 0 |
| 1 — Cœur RDV | 8 | 3 | 1 |
| 2 — Notifications | 5 | 1 | 3 |
| 3 — Spécificités kiné | 4 | 0 | 0 |
| 4 — Exploitation | 7 | 1 | 1 |
