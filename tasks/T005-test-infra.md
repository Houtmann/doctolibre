# T005 — Infra de tests

- **Phase** : 0
- **Dépend de** : T002
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Écrire un test d'intégration DB doit prendre 3 lignes et être isolé et rapide.

## Contexte à lire
`AGENTS.md` (section Tests), `internal/store/db.go`, `internal/store/migrate.go`.

## Spécification
- `internal/testutil/db.go` :
  - `testutil.DB(t) *pgxpool.Pool` : se connecte à `DOCTOLIBRE_TEST_DATABASE_URL` (défaut : base `doctolibre_test` du compose dev),
    puis isole chaque test dans **un schéma dédié** (décision figée, pas de `CREATE DATABASE … TEMPLATE`) :
    1. `CREATE SCHEMA test_<id>` ;
    2. un `pgxpool` **propre au test** dont la config fixe `search_path = test_<id>, public` (`ConnConfig.RuntimeParams`) — jamais de `SET search_path` sur une connexion partagée ;
    3. migrations goose appliquées dans ce schéma (table de version goose dans le schéma du test) ;
    4. `t.Cleanup` : fermer le pool puis `DROP SCHEMA test_<id> CASCADE`.
  - Les extensions (`btree_gist`, `citext`, plus tard `pg_trgm`, `unaccent`) sont créées **une fois** dans `public` (`sync.Once`) ; le `CREATE EXTENSION IF NOT EXISTS` de la migration 00001 devient alors un no-op. Le SQL de l'app ne qualifie jamais les noms par un schéma.
  - Si le coût des migrations par test devient gênant (> 200 ms), mesurer avant d'optimiser et noter le chiffre dans `docs/decisions.md`.
  - Si la variable d'env n'est pas joignable : `t.Skip` avec un message clair (les tests unitaires purs tournent toujours).
- `internal/testutil/clock.go` : interface `Clock { Now() time.Time }` + `FixedClock`. Le vrai `Clock` est dans `internal/domain/clock.go`.
- `internal/testutil/fixtures.go` : constructeurs vides pour l'instant (remplis par T010).
- `make test` lance tout ; sans Postgres, les tests DB sont skippés automatiquement (pas de build tag).
- CI GitHub Actions `.github/workflows/ci.yml` : service postgres, `make check`.

## Critères d'acceptation
- [x] Deux tests DB en parallèle (`t.Parallel()`) ne se voient pas mutuellement.
- [x] Aucun schéma `test_%` ne reste après `make test` (vérification dans le test lui-même ou en CI).
- [ ] CI verte sur une PR.

## Vérification
```bash
make db-up && make test
```

## Hors scope
Fixtures métier (T010).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : `testutil.DB` (schéma `test_<hex>`, pool propre avec search_path en RuntimeParams, extensions en `sync.Once` dans public, goose_db_version vérifiée dans le schéma du test, drop en Cleanup) ; `domain.Clock`/`SystemClock` ; `testutil.FixedClock` (+ Advance) ; `fixtures.go` vide ; `migrate_test.go` migré sur `testutil.DB` ; tests `db_test.go` (3 sous-tests parallèles, schéma supprimé après test, goose dans le bon schéma) ; `.github/workflows/ci.yml` (postgres:17, setup-go go-version-file, golangci-lint, make check). Migration ~40-70 ms par test (< 200 ms). Skip vérifié avec une URL injoignable.
- **Revue (corrections)** : création des extensions protégée par `pg_advisory_xact_lock` (course inter-process entre packages) ; `deploy/initdb/01-test-db.sql` monté dans le compose dev (crée `doctolibre_test`, effectif sur volume neuf : `make db-reset` si le volume existe déjà).
- **Reste à faire** : critère « CI verte sur une PR » (pas de remote, non vérifiable).
- **Notes de reprise** : URL par défaut = identifiants du compose dev mais base `doctolibre_test` (créée par deploy/initdb). Lint gosec G101 neutralisé par `//nolint` sur la constante.
- **Écarts avec la spécification** : l'interface `Clock` n'est pas dupliquée dans testutil (seulement `FixedClock` qui implémente `domain.Clock`).
