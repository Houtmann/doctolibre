# T001 — Bootstrap du dépôt Go

- **Phase** : 0
- **Dépend de** : —
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Un dépôt Go qui compile, se lint, et expose un binaire `doctolibre` avec sous-commandes.

## Contexte à lire
`PLAN.md` §2 et §6, `AGENTS.md`.

## Spécification
- `go mod init github.com/<org>/doctolibre` (laisser `<org>` = `doctolibre` si inconnu), Go ≥ 1.24.
- `cmd/doctolibre/main.go` : sous-commandes via `flag` stdlib (pas de cobra) :
  `serve`, `migrate`, `create-user`, `version`. Seule `version` fonctionne ; les autres affichent « not implemented » et exit 2.
- `internal/config/config.go` : struct `Config` chargée depuis l'env avec valeurs par défaut :
  `DOCTOLIBRE_HTTP_ADDR` (`:8080`), `DOCTOLIBRE_DATABASE_URL`, `DOCTOLIBRE_BASE_URL`,
  `DOCTOLIBRE_SECRET_KEY` (obligatoire hors dev, ≥ 32 octets), `DOCTOLIBRE_ENV` (`dev|prod`),
  `DOCTOLIBRE_TIMEZONE` (`Europe/Paris`). Fonction `Load() (Config, error)` + validation.
  Chargement d'un fichier `.env` en dev uniquement (petit parseur maison, pas de dépendance).
- `Makefile` : `build`, `run`, `test`, `lint`, `fmt`, `check` (= fmt-check + vet + lint + test).
- `.golangci.yml` minimal (errcheck, govet, staticcheck, revive, gosec).
- `.gitignore`, `.env.example`, `README.md` court (pitch + `make run`), `LICENSE` **AGPL-3.0**.
- `version` injectée via `-ldflags "-X main.version=..."`.

## Critères d'acceptation
- [x] `make build` produit `bin/doctolibre` ; `bin/doctolibre version` affiche la version.
- [x] `make check` passe.
- [x] `config.Load` testée : défauts, valeur invalide, secret manquant en prod.

## Vérification
```bash
make check && ./bin/doctolibre version
```

## Hors scope
Serveur HTTP (T003), base de données (T002).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : tout le périmètre (go.mod, main, config + tests, Makefile, golangci, .gitignore, .env.example, README, LICENSE AGPL-3.0).
- **Reste à faire** : rien.
- **Notes de reprise** : config.LoadFrom(getenv) est testable ; Load() ajoute le repli .env en dev.
- **Écarts** : module `doctolibre`, go 1.24 ; LICENSE téléchargée depuis gnu.org.
