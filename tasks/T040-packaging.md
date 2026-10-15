# T040 — Packaging Docker + Caddy

- **Phase** : 4
- **Dépend de** : T004, T010
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Un praticien (ou son informaticien) installe Doctolibre sur un VPS avec `docker compose up -d`, TLS automatique inclus.

## Contexte à lire
`Makefile`, `cmd/doctolibre/main.go`, `internal/config/config.go`, `.env.example`.

## Spécification
- `deploy/Dockerfile` : multi-stage, build `CGO_ENABLED=0 -trimpath -ldflags "-s -w -X main.version=…"`, image finale `gcr.io/distroless/static:nonroot`, ≤ 30 Mo. `ENTRYPOINT ["/doctolibre"]`, `CMD ["serve"]`. Migrations appliquées au démarrage si `DOCTOLIBRE_AUTO_MIGRATE=true` (défaut).
- `deploy/docker-compose.yml` (prod) : `app`, `postgres:17-alpine` (volume, healthcheck, pas de port exposé), `caddy:2-alpine` (80/443, `Caddyfile` avec `{$DOMAIN}`), réseau interne, `restart: unless-stopped`, limites mémoire (app 256 Mo, pg 512 Mo).
- `deploy/.env.example` documenté ligne par ligne.
- Variante sans Docker : binaire + unit **systemd** (`deploy/doctolibre.service`) + Postgres du système.
- GitHub Actions `release.yml` : sur tag `v*`, binaires linux amd64/arm64 + image multi-arch sur GHCR.
- Cible arm64 testée (Raspberry Pi 4/5 = option « au cabinet »).

## Critères d'acceptation
- [ ] `docker compose up` sur une VM vierge → site en HTTPS (test avec un domaine réel ou `tls internal` en local).
- [ ] Empreinte mémoire au repos de l'app < 50 Mo (noter la mesure).
- [ ] Redémarrage de la VM → tout revient seul.

## Vérification
```bash
docker build -f deploy/Dockerfile -t doctolibre:dev . && docker compose -f deploy/docker-compose.yml config
```

## Hors scope
Kubernetes, Helm.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
