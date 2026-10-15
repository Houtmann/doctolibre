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
- [ ] `DOCTOLIBRE_WORKER=off` : aucun worker ne démarre (test).
- [ ] Ctrl-C arrête serveur et worker proprement (log de fin des deux).
- [ ] `/admin/messages` : 303 vers `/login` si non authentifié ; liste avec destinataires masqués si authentifié (test).
- [ ] « Renvoyer » remet un message `failed` en `pending` (test).

## Vérification
```bash
make check
```

## Hors scope
Les drivers email et SMS (T021 et T022) ; les alertes admin (T046).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** : —
- **Écarts avec la spécification** : —
