# État du projet

> Tableau de bord **réécrit à la fin de chaque session** (pas d'historique ici : voir [`journal.md`](journal.md)).
> Le statut détaillé de chaque tâche est dans [`tasks/README.md`](../../tasks/README.md), seule source de vérité.

**Mis à jour le** : 2026-10-15

## Où on en est

- **Phase 0 (Fondations) terminée.** Le binaire démarre (`serve`, `migrate`, `create-user`, `version`), avec la page d'accueil et le 404 stylé, et les tests DB isolés par schéma.
- **Cœur RDV en cours** :
  - schéma v1 avec la contrainte anti-chevauchement buffers inclus (T010) ;
  - moteur de créneaux pur, testé sur les changements d'heure et par fuzz (T013) ;
  - connexion praticien (T011).
- **Notifications** : l'outbox et le worker existent (T020), mais le worker n'est pas encore lancé par le binaire (T024).
- **Dépôt** : branche `main`, un commit et un merge par tâche. Il y a 29 tâches : T024 et T046 ont été créées pour ce qui n'a pas pu être fait dans T020 et T045.

## En cours

_Aucune tâche en cours._

## Prochaine action

Tâches prêtes (dépendances `done`), parallélisables :
1. [T012 — Paramétrage cabinet, motifs, disponibilités](../../tasks/T012-availability-admin.md) : débloque T014 et T016, donc le MVP.
2. [T021 — Email SMTP](../../tasks/T021-email-smtp.md) et [T022 — SMS](../../tasks/T022-sms-providers.md), en parallèle.
3. [T024 — Câblage du worker outbox et page admin](../../tasks/T024-outbox-wiring.md).
4. [T040 — Packaging Docker + Caddy](../../tasks/T040-packaging.md) : inclut maintenant la gestion de l'IP client derrière le proxy.

## Blocages et questions ouvertes pour l'humain

- Nom de l'organisation et du module Go : `doctolibre` provisoirement.
- Hébergement du dépôt (GitHub, Codeberg ou GitLab) : la CI (`.github/workflows/ci.yml`) est écrite mais n'a jamais tourné, faute de remote.
- Vérifications manuelles restantes :
  - Lighthouse accessibilité ≥ 95 sur `/` (T004) ;
  - `make db-up` avec Docker (T002, T005) : le daemon Docker n'était pas lancé, tout a été vérifié sur le Postgres.app local.
- Relecture humaine des 10 tâches marquées `done` : contrôlées par Opus (`make check`, diff, revue renforcée pour T010, T011, T013 et T020), mais pas encore relues par un humain.

## Avancement par phase

| Phase | Tâches | Faites |
|---|---|---|
| 0 — Fondations | 5 | 5 |
| 1 — Cœur RDV | 8 | 3 |
| 2 — Notifications | 5 | 1 |
| 3 — Spécificités kiné | 4 | 0 |
| 4 — Exploitation | 7 | 1 |
