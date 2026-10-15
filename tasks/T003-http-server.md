# T003 — Serveur HTTP et middlewares

- **Phase** : 0
- **Dépend de** : T001
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
`doctolibre serve` démarre un serveur HTTP robuste, avec arrêt propre et middlewares de base.

## Contexte à lire
`AGENTS.md`, `cmd/doctolibre/main.go`, `internal/config/config.go`.

## Spécification
- `internal/httpx/server.go` : `New(cfg, handler) *http.Server` avec timeouts (read header 5s, read 15s, write 30s, idle 60s).
  Arrêt gracieux sur SIGINT/SIGTERM (`signal.NotifyContext`, `Shutdown` 10s).
- `internal/httpx/middleware.go`, chaînables (`func(http.Handler) http.Handler`) :
  - `RequestID` (header `X-Request-ID`, mis dans le contexte et le logger),
  - `Logger` (slog : méthode, chemin, statut, durée, request id — **jamais** de query string ni body),
  - `Recover` (500 + log de la stack),
  - `SecureHeaders` (CSP stricte `default-src 'self'`, `X-Frame-Options: DENY`, `Referrer-Policy: same-origin`, HSTS si prod),
  - `CSRF` : double-submit cookie ou jeton en session ; vérif sur toute méthode non-GET ; header `HX-Request`-compatible (`X-CSRF-Token`).
  - `RateLimit(key func(*http.Request) string, rate, burst)` en mémoire (`golang.org/x/time/rate` + map avec éviction).
- `internal/web/routes.go` : `Routes(deps) http.Handler` utilisant `http.ServeMux` avec patterns Go 1.22.
  Routes : `GET /healthz` (200 `ok`, ping DB si dispo), `GET /` (placeholder texte).
- `internal/httpx/respond.go` : helpers `Error(w, r, status, msg)`, `Redirect`, détection htmx (`IsHTMX(r)`).

## Critères d'acceptation
- [x] `doctolibre serve` répond sur `/healthz`, s'arrête proprement sur Ctrl-C.
- [x] Tests `httptest` : request id propagé, recover renvoie 500, CSRF rejette un POST sans jeton, rate limit renvoie 429.
- [x] Headers de sécurité présents sur toutes les réponses.

## Vérification
```bash
make check && (./bin/doctolibre serve & sleep 1; curl -fsS localhost:8080/healthz; kill %1)
```

## Hors scope
Templates HTML (T004), auth (T011).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : `internal/httpx` (server.go New+Run, middleware.go RequestID/Logger/Recover/SecureHeaders/CSRF/RateLimit/Chain, respond.go), `internal/web/routes.go` (Routes, /healthz avec Pinger optionnel, GET /{$}), `cmd/doctolibre/serve.go` + case "serve" dans main.go, tests httptest. `make check` vert, `serve` + healthz + SIGINT vérifiés.
- **Reste à faire** : rien.
- **Notes de reprise** : CSRF = double-submit cookie `csrf_token` (HttpOnly) ; token lisible via `httpx.CSRFToken(ctx)` pour les templates (champ `csrf_token` ou header `X-CSRF-Token`). `ClientIP` n'utilise pas X-Forwarded-For (à traiter derrière Caddy, T040). Le middleware RateLimit n'est pas encore branché sur des routes (login/OTP/réservation à venir). `Deps.DB` attend un `Pinger` (le pool pgx le satisfait) ; non câblé dans `serve` (pas de DB ici).
- **Écarts avec la spécification** : x/time v0.14.0 (seule version en cache compatible go 1.24) ; la ligne `serve` du test `TestRun` retirée (plus « not implemented »).
