# Composants tiers

| Composant | Version | Licence | Chemin dans le dépôt |
|---|---|---|---|
| htmx | 2.0.4 | Zero-Clause BSD | `internal/web/static/htmx.min.js`, licence : `internal/web/static/vendor/LICENSE-htmx` |
| EventCalendar (vkurko) | 5.15.0 | MIT | `internal/web/static/vendor/event-calendar.min.{js,css}`, licence : `internal/web/static/vendor/LICENSE-event-calendar` |
| daisyUI | 5.7.47 | MIT | `tools/daisyui/` (licence : `tools/daisyui/LICENSE`) |
| Tailwind CSS (exécutable autonome) | 4.3.3 | MIT | téléchargé dans `bin/` par `tools/fetch-tailwind.sh` (sha256 épinglé, non versionné) ; sortie générée : `internal/web/static/ui.css` (en-tête MIT conservé) |

## Dépendances Go directes (`go.mod`)

Licences relevées dans le cache de modules (`go env GOMODCACHE`).

| Module | Version | Licence |
|---|---|---|
| github.com/google/uuid | v1.6.0 | BSD-3-Clause |
| github.com/jackc/pgx/v5 | v5.11.0 | MIT |
| github.com/pressly/goose/v3 | v3.29.0 | MIT |
| github.com/prometheus/client_golang | v1.22.0 | Apache-2.0 |
| golang.org/x/crypto | v0.58.0 | BSD-3-Clause |
| golang.org/x/term | v0.47.0 | BSD-3-Clause |
| golang.org/x/time | v0.14.0 | BSD-3-Clause |

Outil de développement (directive `tool`, non lié au binaire) : `github.com/sqlc-dev/sqlc` v1.31.1, licence MIT (non présent dans le cache local lors de la vérification ; licence connue d'après le dépôt amont).

Les dépendances indirectes (`// indirect`) sont en grande partie tirées par les outils goose et sqlc ; leurs licences n'ont pas été relevées une à une.
