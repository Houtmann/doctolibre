# T004 — Socle front : templates, htmx, CSS

- **Phase** : 0
- **Dépend de** : T003
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Un système de rendu HTML serveur simple, embarqué dans le binaire, avec htmx et un CSS sobre, accessible et mobile-first.

## Contexte à lire
`AGENTS.md` (section Front), `internal/web/routes.go`, `internal/httpx/respond.go`.

## Spécification
- `internal/web/static/` : `htmx.min.js` (version épinglée, vendored), `pico.min.css` (vendored), `app.css` (surcharges, variables de couleur), `app.js` (vide ou presque).
  Servi sous `/static/` avec `embed.FS`, cache long + empreinte (`/static/app.css?v=<hash>` calculé au démarrage).
- `internal/web/templates/` :
  - `layout/base.html` (doctype, `lang="fr"`, meta viewport, CSRF en `<meta>` + `hx-headers`),
  - `layout/public.html` et `layout/admin.html` (blocs `title`, `content`),
  - `partials/flash.html`, `partials/form_errors.html`,
  - `pages/home.html`, `pages/error.html`.
- `internal/web/render.go` : `Renderer` qui parse tout au démarrage (rechargement à chaud si `ENV=dev`),
  méthodes `Page(w, r, name, data)` et `Partial(w, r, name, data)` (pour réponses htmx).
  `FuncMap` : `formatDate`, `formatTime`, `formatDuration` (fr-FR, fuseau du cabinet), `dict`, `csrf`.
- Flash messages via cookie signé (clé `SECRET_KEY`).
- Pas de Node, pas de bundler. Pas de framework JS. Alpine.js **seulement** si un besoin réel apparaît plus tard.
- Accessibilité : labels sur tous les champs, focus visible, contrastes AA.

## Critères d'acceptation
- [x] `/` rend une page d'accueil via le layout public, sans erreur console, CSP respectée (pas de script inline). _(vérifié par Opus dans le navigateur, 2026-10-15)_
- [x] Une page d'erreur 404 / 500 stylée.
- [x] Test : chaque template parse ; rendu de `home` contient le titre attendu.
- [ ] Lighthouse mobile accessibilité ≥ 95 sur `/` (vérif manuelle, noter le score dans la PR).

## Vérification
```bash
make check && make run   # puis ouvrir http://localhost:8080
```

## Hors scope
Pages métier (T012+).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : static (htmx 2.0.4, pico 2.0.6, app.css, app.js), templates (layout base/public/admin, partials flash/form_errors, pages home/error), `internal/web/render.go` (Renderer, Page/PageStatus/Partial/Error, flash cookie signé HMAC, FuncMap, StaticHandler avec ?v=empreinte), routes (/, /static/, 404 stylé, panic -> 500 stylé via `httpx.RecoverWith`), tests `render_test.go`.
- **Vendored** (jsdelivr, versions fixes) : `htmx.org@2.0.4/dist/htmx.min.js` sha256 `e209dda5c8235479f3166defc7750e1dbcd5a5c1808b7792fc2e6733768fb447` ; `@picocss/pico@2.0.6/css/pico.min.css` sha256 `dd5fd5591afd81ee21dcc117ad85c014dc3f1f19dc2d7b7d101ea0acc29274c2`.
- **Reste à faire** : vérif manuelle navigateur (console sans erreur CSP) et score Lighthouse mobile a11y >= 95 sur `/` (`make run`).
- **Notes de reprise** : les templates reçoivent `View{CSRF, Flash, Data}` (`.Data` = donnée de la page) ; `Partial` passe la donnée brute. Layout d'une page : première ligne `{{/* layout: admin */}}` (défaut public). Formulaire : `{{csrf .CSRF}}`. htmx : `hx-headers` sur <body> + meta htmx-config (includeIndicatorStyles/allowEval off pour la CSP ; styles d'indicateur dans app.css). Rechargement à chaud si env != prod, depuis `internal/web/templates` quand le cwd est la racine du dépôt.
- **Écarts avec la spécification** : `csrf` prend le jeton (pas la requête) ; rechargement à chaud selon `DOCTOLIBRE_ENV` (pas `ENV`) ; `httpx.Recover` conservé, ajout de `httpx.RecoverWith` (middleware.go) ; fuseau = `Config.Timezone` (cabinet pas encore en base) ; critère 1 non coché (pas de vérif console navigateur).
