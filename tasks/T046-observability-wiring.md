# T046 — Observabilité : branchements et /admin/status

- **Phase** : 4
- **Dépend de** : T045, T014, T020, T041
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Terminer ce que T045 n'a pas pu faire faute de code dans `main` : brancher les métriques sur la DB, l'outbox et la réservation, et ajouter la page d'état et les alertes admin.

## Contexte à lire
`AGENTS.md`, `tasks/T045-observability.md` (spec d'origine et section Suivi), `internal/metrics/`, `cmd/doctolibre/serve.go`, `internal/web/routes.go`.

## Spécification
- `serve.go` : appeler `RegisterPool` (stats du `pgxpool`) et `RegisterOutbox` (comptes par statut, âge du plus vieux `pending`, requête indexée) quand la DB et l'outbox sont configurées.
- Service de réservation : incrémenter `AppointmentsCreated{source}` après le commit de la transaction, jamais avant.
- `/admin/status` (rôle admin) : version, uptime, version de migration, taille de la DB, messages `failed`, date du dernier backup (fichier marqueur de T041).
- Alertes email admin via l'outbox, dédoublonnées par `dedup_key` : des messages `failed` dans la dernière heure ; aucun backup depuis 48 h.
- `TestNoPIIInLogs` : étendre au parcours complet de réservation (T014).
- Documenter `DOCTOLIBRE_METRICS_TOKEN` dans `.env.example`.

## Critères d'acceptation
- [ ] `/metrics` expose les métriques outbox et pool sur une instance avec DB.
- [ ] `/admin/status` : 403 pour un non-admin, page complète pour un admin.
- [ ] Une alerte n'est émise qu'une fois par fenêtre (test).
- [ ] Aucune PII dans les logs d'une réservation complète (test).

## Vérification
```bash
make check
```

## Hors scope
Tableaux de bord Grafana, traces, alerting externe.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** : —
- **Écarts avec la spécification** : —
