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
- [ ] `/` rend une page d'accueil via le layout public, sans erreur console, CSP respectée (pas de script inline).
- [ ] Une page d'erreur 404 / 500 stylée.
- [ ] Test : chaque template parse ; rendu de `home` contient le titre attendu.
- [ ] Lighthouse mobile accessibilité ≥ 95 sur `/` (vérif manuelle, noter le score dans la PR).

## Vérification
```bash
make check && make run   # puis ouvrir http://localhost:8080
```

## Hors scope
Pages métier (T012+).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
