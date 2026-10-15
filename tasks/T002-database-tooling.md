# T002 — PostgreSQL dev, goose, sqlc

- **Phase** : 0
- **Dépend de** : T001
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Une base Postgres locale en une commande, des migrations embarquées dans le binaire, et sqlc configuré.

## Contexte à lire
`AGENTS.md` (section SQL), `cmd/doctolibre/main.go`, `internal/config/config.go`.

## Spécification
- `deploy/docker-compose.dev.yml` : service `postgres:17-alpine`, port 5432, volume nommé, user/db `doctolibre`.
- `make db-up`, `make db-down`, `make db-reset`, `make migrate`, `make sqlc`.
- `migrations/` : fichiers goose SQL `NNNNN_nom.sql`. Première migration `00001_init.sql` :
  `CREATE EXTENSION IF NOT EXISTS btree_gist; CREATE EXTENSION IF NOT EXISTS citext;`
- `migrations/embed.go` : `//go:embed *.sql` exposé en `embed.FS`.
- `internal/store/db.go` : `Open(ctx, url) (*pgxpool.Pool, error)` avec ping et timeouts raisonnables.
- `internal/store/migrate.go` : `Migrate(ctx, pool)` via goose (provider API, dialecte postgres, depuis l'`embed.FS`).
- Sous-commande `doctolibre migrate [up|down|status]` fonctionnelle.
- `sqlc.yaml` : engine postgresql, `queries: internal/store/queries`, `schema: migrations`,
  sortie `internal/store/sqlcgen`, `sql_package: pgx/v5`, `emit_json_tags: false`,
  overrides : `uuid` → `github.com/google/uuid.UUID`, `timestamptz` → `time.Time`.
- Fichier de requête bidon `internal/store/queries/health.sql` (`-- name: Ping :one SELECT 1;`) pour valider la génération.
- Outils (goose, sqlc) installés via `go tool` (directive `tool` dans `go.mod`) — pas d'install globale requise.

## Critères d'acceptation
- [ ] `make db-up && make migrate` fonctionne sur une machine vierge avec Docker.
- [ ] `make sqlc` génère du code qui compile ; le code généré est commité.
- [ ] `doctolibre migrate status` liste la migration init.

## Vérification
```bash
make db-up && make migrate && make sqlc && make check
```

## Hors scope
Tables métier (T010), helpers de test (T005).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
