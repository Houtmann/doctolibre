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
- [x] `/metrics` expose les métriques outbox et pool sur une instance avec DB.
- [x] `/admin/status` : 403 pour un non-admin, page complète pour un admin.
- [x] Une alerte n'est émise qu'une fois par fenêtre (test).
- [x] Aucune PII dans les logs d'une réservation complète (test).

## Vérification
```bash
make check
```

## Hors scope
Tableaux de bord Grafana, traces, alerting externe.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : `registerDBMetrics` (serve.go) : `RegisterOutbox` + `RegisterPool` ; `OnCreated(source)` sur `service.Booking` ("online") et `service.Calendar` ("staff"), appelé après commit, branché sur `AppointmentsCreated` quand les métriques sont actives. `service.Status` + page `/admin/status` (rôle admin : version, uptime, version de migration, taille DB, messages failed, dernière sauvegarde). `service.Alerts` (tâche périodique 10 min lancée par `startWorker`) : emails `admin_alert` aux admins, dédoublonnés par `dedup_key` (heure UTC pour les échecs de la dernière heure, jour UTC pour la sauvegarde > 48 h). Migration 00017 (index partiel `outbox_message_failed_idx`). `DOCTOLIBRE_BACKUP_DIR` (config, compose, `.env.example`) ; `DOCTOLIBRE_METRICS_TOKEN` était déjà documenté. Tests : `internal/web/observability_test.go` (parcours de réservation complet sans PII + compteur, page status 303/403/200), `internal/service/alerts_test.go`, `cmd/doctolibre/metrics_wiring_test.go`.
- **Reste à faire** : rien dans le périmètre.
- **Notes de reprise** : dernière sauvegarde = fichier `.last_success` écrit par `deploy/backup/backup.sh` ; le volume `backups` est monté en lecture seule (`/backups:ro`) dans le service `app` et `DOCTOLIBRE_BACKUP_DIR=/backups` l'active (vide = pas de suivi ni d'alerte). Un cabinet sans marqueur n'est alerté qu'après 48 h d'existence. `schema_test.go` : borne de la boucle de descente de migrations portée de 10 à 40 (elle cassait avec la 00017).
- **Écarts avec la spécification** : alerte « failed » = échecs dont `updated_at` est dans la dernière heure (hors messages `admin_alert`). `CarePlans` (séries) ne compte pas encore dans `AppointmentsCreated`. Pas de lien vers `/admin/status` dans le tableau de bord (page partagée par tous les rôles). EXPLAIN non exécuté (base de test vide) ; les comptages s'appuient sur l'index partiel `outbox_message_failed_idx`.
