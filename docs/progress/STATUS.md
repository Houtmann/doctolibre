# État du projet

> Tableau de bord **réécrit à la fin de chaque session** (pas d'historique ici : voir [`journal.md`](journal.md)).
> Le statut détaillé de chaque tâche est dans [`tasks/README.md`](../../tasks/README.md), seule source de vérité.

**Mis à jour le** : 2026-10-15

## Où on en est

- **Phase** : 0 — Fondations (pas commencée)
- **Code** : aucun. Le plan, les conventions et les 27 tâches sont écrits et ont été revus (review externe intégrée).
- **Dépôt** : git initialisé (branche `main`), commit initial de la documentation `629f231`.

## En cours

_Aucune tâche en cours._

<!-- Format :
- [Txxx](../../tasks/Txxx-....md) — session du AAAA-MM-JJ — arrêté à : <étape> — reprise : voir section « Suivi » de la tâche
-->

## Prochaine action

1. ~~Premier commit de la documentation~~ (fait).
2. `/run-task T001` — [T001 — Bootstrap du dépôt Go](../../tasks/T001-bootstrap.md).
3. Ensuite, en parallèle : [T002](../../tasks/T002-database-tooling.md), [T003](../../tasks/T003-http-server.md), [T013](../../tasks/T013-slot-engine.md).

## Blocages et questions ouvertes pour l'humain

- Nom de l'organisation / du module Go (`github.com/<org>/doctolibre`) — T001 utilisera `doctolibre` par défaut.
- Hébergement du dépôt (GitHub, Codeberg, GitLab ?) — impacte la CI (T005) et la release (T040).

## Avancement par phase

| Phase | Tâches | Faites |
|---|---|---|
| 0 — Fondations | 5 | 0 |
| 1 — Cœur RDV | 8 | 0 |
| 2 — Notifications | 4 | 0 |
| 3 — Spécificités kiné | 4 | 0 |
| 4 — Exploitation | 6 | 0 |
