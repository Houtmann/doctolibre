# T045 — Observabilité légère

- **Phase** : 4
- **Dépend de** : T003
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Savoir si l'instance va bien sans déployer une stack de monitoring.

## Contexte à lire
`internal/httpx/middleware.go`, `internal/outbox/worker.go`.

## Spécification
- `/metrics` Prometheus (`prometheus/client_golang`), désactivé par défaut, protégé par token ou IP : durée des requêtes par route (pattern, pas chemin brut), RDV créés, messages outbox par statut, âge du plus vieux message `pending`, pool pgx.
- `/admin/status` (admin) : version, uptime, migrations, taille DB, messages en échec, dernier backup réussi.
- Alerte email à l'admin si : messages `failed` > 0 dans la dernière heure, ou pas de backup depuis 48 h (job quotidien du worker).
- Logs : `slog` JSON en prod, texte en dev ; **aucune donnée patient dans les logs** (test qui vérifie les logs d'une réservation complète).

## Critères d'acceptation
- [ ] Test « pas de PII dans les logs ».
- [ ] `/metrics` inaccessible sans token.

## Vérification
```bash
make check
```

## Hors scope
Tracing distribué.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
