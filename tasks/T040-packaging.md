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
- **IP client derrière Caddy** (remonté par T011) : `httpx.ClientIP` n'utilise que `RemoteAddr`, donc derrière le proxy toutes les requêtes ont l'IP de Caddy et le rate limit « par IP » devient global. Ajouter `DOCTOLIBRE_TRUSTED_PROXIES` (CIDR) : `X-Forwarded-For` n'est lu que si `RemoteAddr` est dans cette liste (prendre l'adresse la plus à droite non fiable). Test obligatoire.
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

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : `DOCTOLIBRE_TRUSTED_PROXIES` (`config.ParseCIDRs`, erreur au démarrage) et `DOCTOLIBRE_AUTO_MIGRATE` (défaut true, `serve`) ; middleware `httpx.TrustedProxies` (premier de la chaîne dans `web.Routes`) qui range l'IP résolue dans le contexte, lue par `httpx.ClientIP` (donc rate limit login et audit auth l'utilisent ; le logger de requêtes ne journalise pas d'IP) ; sous-commande `doctolibre healthcheck` ; `deploy/{Dockerfile,docker-compose.yml,Caddyfile,.env.example,doctolibre.service}`, `.dockerignore`, `.github/workflows/release.yml`. Tests : table-driven ClientIP (forgé, multi-XFF, IPv6, espaces, invalide), config, healthcheck. `make check` vert. `docker compose config` OK (avec un .env temporaire, supprimé). Binaires CGO_ENABLED=0 -trimpath : amd64 16,9 Mo, arm64 15,9 Mo.
- **Reste à faire** (vérifs manuelles, le daemon Docker s'est arrêté pendant la session) : `docker build -f deploy/Dockerfile -t doctolibre:dev .` et taille de l'image (attendue ~18 Mo, limite 30) ; `caddy validate` du Caddyfile (notamment `tls {$CADDY_TLS}` (variable désormais obligatoire : email ACME ou internal)) ; `docker compose up` avec `DOMAIN=localhost CADDY_TLS=internal` -> HTTPS ; mesure mémoire au repos de l'app (< 50 Mo) via `docker stats` ; redémarrage VM ; test arm64 sur Raspberry Pi ; vérifier que X-Forwarded-For posé par Caddy est bien pris (sous-réseau 172.30.0.0/24 fixé dans le compose) ; release.yml jamais exécuté.
- **Notes de reprise** : le `.env` de compose est `deploy/.env` (gitignoré). Variables SMTP/SMS (T021/T022) à ajouter dans `.env.example` et dans `environment:` de l'app après merge. Image par défaut `ghcr.io/doctolibre/doctolibre` : à adapter au vrai dépôt.
- **Écarts avec la spécification** : `.env.example` de deploy documente aussi les variables fixées par le compose. `X-Forwarded-For` : parcours droite-gauche (première IP non fiable) comme demandé. Healthcheck ajouté (Dockerfile + compose). Aucun test web de bout en bout sur le rate limit derrière proxy (test au niveau httpx uniquement).
