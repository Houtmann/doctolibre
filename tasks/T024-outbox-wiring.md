# T024 — Outbox : câblage du worker et page admin

- **Phase** : 2
- **Dépend de** : T020, T011
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Faire tourner le worker de T020 dans le binaire et donner au cabinet une vue des messages envoyés ou en échec.

## Contexte à lire
`AGENTS.md`, `tasks/T020-outbox-worker.md` (spec et Suivi), `internal/outbox/`, `cmd/doctolibre/serve.go`, `internal/web/routes.go`, l'auth de T011.

## Spécification
- `serve` : lancer `outbox.Worker.Run` dans une goroutine, sauf si `DOCTOLIBRE_WORKER=off`. Arrêt gracieux avec le serveur HTTP, en attendant la fin du worker.
- Sous-commande `doctolibre worker` : le worker seul, sans HTTP.
- Tant que T021 et T022 n'existent pas, utiliser un `Dispatcher` « log » qui ne logge que l'ID et le kind.
- `metrics.RegisterOutbox(outbox.Queue{…})` quand les métriques sont actives.
- `/admin/messages` (staff authentifié) : 50 derniers messages du cabinet via `Queue.ListRecent`, avec statut, tentatives et erreur. Le destinataire est masqué (`06•••••12`, `j•••@exemple.fr`). Un bouton « Renvoyer » (POST avec CSRF, puis redirect) appelle `Queue.Resend`.

## Critères d'acceptation
- [x] `DOCTOLIBRE_WORKER=off` : aucun worker ne démarre (test).
- [x] Ctrl-C arrête serveur et worker proprement (log de fin des deux).
- [x] `/admin/messages` : 303 vers `/login` si non authentifié ; liste avec destinataires masqués si authentifié (test).
- [x] « Renvoyer » remet un message `failed` en `pending` (test).

## Vérification
```bash
make check
```

## Hors scope
Les drivers email et SMS (T021 et T022) ; les alertes admin (T046).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** :
  - `config.WorkerEnabled` (`DOCTOLIBRE_WORKER=on|off`, défaut on, autre valeur = erreur).
  - `cmd/doctolibre/dispatch.go` : `newDispatcher(email, sms, logger)` route par kind ; nil => dispatcher « log » (ID + kind seulement) ; kind inconnu => erreur `Permanent`. L'orchestrateur n'a qu'à passer les dispatchers réels dans `serve.go` / `worker.go` (2 appels `newDispatcher(nil, nil, logger)`).
  - `cmd/doctolibre/worker.go` : `startWorker` (goroutine + `sync.WaitGroup`, logs « démarré/arrêté ») et sous-commande `doctolibre worker`.
  - `serve.go` : worker lancé avec le contexte du serveur ; après `httpx.Run` (Shutdown), `stop()` puis attente du worker avant fermeture du pool ; `RegisterOutbox(outbox.Queue{…})` si métriques actives ; `deps.Messages` renseigné.
  - `internal/web/messages.go` + `templates/pages/admin_messages.html` : `GET /admin/messages` (50 derniers, filtré par cabinet de l'utilisateur), `POST /admin/messages/{id}/resend` (CSRF global, 303, 404 si message d'un autre cabinet / inconnu / non renvoyable / id invalide). Destinataire masqué côté Go (`maskRecipient`), y compris dans `last_error`. Lien ajouté sur le tableau de bord.
  - Migration `00005_outbox_status_idx.sql` : index sur `status` pour `OutboxCountByStatus` (métriques).
  - Tests : `cmd/doctolibre/worker_test.go` (off, run+arrêt, routage, pas de PII), `internal/web/messages_test.go`, `mask_internal_test.go`, config.
- **EXPLAIN** (300 000 lignes, 3 cabinets) :
  - Liste : `Limit -> Index Scan using outbox_message_practice_created_idx`, 0,024 ms.
  - Stats : `Parallel Index Only Scan using outbox_message_status_idx` (Heap Fetches 0), 17 ms pour 300 k lignes (reste O(n) : un index-only scan, sans parcours de la table).
  - Plus vieux pending : `Index Only Scan using outbox_message_pending_run_at_idx`, 0,011 ms.
- **Reste à faire** : brancher les vrais dispatchers (T021/T022) ; Ctrl-C de bout en bout non testé à la main (couvert par le test d'arrêt de `startWorker` + code de `serve`).
- **Notes de reprise** : `make check` vert avec `DOCTOLIBRE_TEST_DATABASE_URL` sur Postgres.app.
- **Écarts avec la spécification** :
  - Migration ajoutée (non prévue) pour indexer les stats.
  - `TestMigrateUpDownUp` (internal/store/schema_test.go) supposait que la dernière migration était outbox : il redescend maintenant jusqu'à la disparition d'`outbox_message` (nécessaire dès qu'une migration suit 00004 ; T012 devra en profiter).
  - Numéro 00024 choisi pour éviter une collision avec les migrations de T012 (00003 libre).
