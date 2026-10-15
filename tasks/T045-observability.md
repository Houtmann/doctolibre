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
- [x] Test « pas de PII dans les logs ».
- [x] `/metrics` inaccessible sans token.

## Vérification
```bash
make check
```

## Hors scope
Tracing distribué.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : package `internal/metrics` (registre propre, histogramme durée par méthode/pattern de route/classe de statut, compteur `appointments_created_total{source}`, collecteurs Go/process, points d'extension `RegisterOutbox(OutboxSource)` et `RegisterPool(func() PoolStats)`, handler `/metrics` protégé par jeton Bearer en temps constant). Config `DOCTOLIBRE_METRICS_TOKEN` (vide = désactivé, route absente = 404 ; min 16 octets). Branchement minimal dans `web.Routes` (champ `Deps.Metrics`, middleware autour du mux) et `serve.go` (logger JSON en prod / texte en dev). Tests : `internal/web/metrics_test.go` (accès, désactivé par défaut, label = pattern, extensions, absence de PII dans les logs).
- **Reste à faire** : (1) `/admin/status` (dépend auth/DB/migrations/backup) ; (2) alertes email admin (failed > 0 sur 1 h, pas de backup 48 h) via le worker outbox ; (3) brancher `RegisterOutbox` / `RegisterPool` dans `serve.go` quand DB + outbox existent, et incrémenter `AppointmentsCreated` dans le service de réservation ; (4) étendre `TestNoPIIInLogs` au parcours de réservation complet (T014) ; (5) documenter `DOCTOLIBRE_METRICS_TOKEN` dans `.env.example`/docs (T040/T044).
- **Notes de reprise** : le middleware métriques doit envelopper directement le mux (sinon `r.Pattern` est vide car RequestID copie la requête). Protection par token uniquement (pas d'option IP). client_golang épinglé à v1.22.0 et dépendances rétrogradées pour garder `go 1.24.0` (les versions récentes imposent go 1.25/1.26).
- **Écarts avec la spécification** : `/admin/status`, alertes email, métriques outbox/pool effectives et « dernier backup » non faits (dépendances absentes de main) ; seuls les points d'extension existent. Le test PII porte sur le middleware de requêtes, pas encore sur une réservation complète.
