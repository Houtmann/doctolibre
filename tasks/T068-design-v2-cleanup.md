# T068 — Design v2 : retrait de Pico et de l'ancien CSS

> **Remplacée** par T084 (migration vers Tailwind + daisyUI). Ne pas implémenter.

- **Phase** : 5
- **Dépend de** : T065, T066, T067
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard (+ captures avant/après clair/sombre, 360 et 1280 px)

## Objectif
L'application ne charge plus que `base.css`, `tokens.css`, `components.css`, `grid.css` (+ `app.css` résiduel s'il reste des styles propres à une page, sans couleur ni jeton `--app-*`). Pico et les jetons v1 ont disparu ; la documentation décrit le v2.

## Contexte à lire
`design/README.md`, `design/MIGRATION.md`, la ou les cartes `design/previews/` citées ci-dessous, `/dev/styleguide/v2` (T063), `layout/base.html`, `internal/web/static/{app.css,pico.min.css,base.css}`, `design/grid.css`, `docs/design-system.md`, `pages/dev_styleguide.html` (v1), `internal/web/styleguide.go`, `AGENTS.md` section Front, `PLAN.md` (ligne Front : Pico CSS).

## Spécification
- **Garde-fou avant suppression** : lister les éléments que Pico stylisait par élément et qui n'ont pas de classe `.dl-*` (`button`, `a[role=button]`, `input` non caché, `select`, `textarea`, `table`, `dialog`, `article`, `nav ul`, `details`, `progress`). Chaque occurrence restante est migrée dans cette tâche, ou couverte par `base.css` si un style par élément suffit (ex. `table` dans un contenu éditorial). Liste et traitement consignés dans Suivi.
- Lier `base.css` ; déplacer les classes `.gr/.gs/.gc` d'`app.css` vers `static/grid.css` (identiques à `design/grid.css`) ; supprimer `pico.min.css` et sa ligne dans le layout.
- `app.css` : supprimer jetons `--app-*` et classes v1 mortes ; ce qui reste est réécrit avec des jetons `--dl-*` ou migré en composant. Si `app.css` devient vide, le supprimer.
- Styleguide : `/dev/styleguide` devient le styleguide v2 (l'ancienne page v1 et `/dev/styleguide/v2` fusionnent ; garder un seul test dev/prod).
- `docs/design-system.md` réécrit pour le v2 (jetons, composants, règles, ratios de `design/README.md`) ; `AGENTS.md` Front : `--dl-*` au lieu de `--app-*` ; `PLAN.md` : « CSS maison (design system v2) » au lieu de Pico. Exception au périmètre exécutant : ces deux modifications sont prévues par la tâche.
- Vérifier la CSP : plus besoin des icônes `data:` de Pico (défaut connu levé).
- Règles communes : remplacer les classes v1 par les classes v2 selon `design/MIGRATION.md` ; les tests `httptest` qui cherchent d'anciennes classes sont mis à jour (fragments v2) ; aucun `style=` ni script inline ; les formulaires marchent toujours sans JS ; les variantes htmx des partials gardent les mêmes `id`/`hx-target`. Si une classe v1 n'est plus utilisée nulle part après la tâche, la retirer d'`app.css` ; sinon la laisser (T068 nettoie).
- Composant manquant ou inadapté : l'ajouter dans `internal/web/static/components.css` **et** `design/components.css` (jetons sémantiques uniquement), et le signaler dans Suivi.

## Critères d'acceptation
- [ ] `grep -rn 'pico\|--app-' internal/ docs/design-system.md AGENTS.md` vide (hors `decisions.md`/journal).
- [ ] Toutes les pages (liste dans `render_test.go`) vérifiées à 360 px sans débordement horizontal, clair et sombre (orchestrateur).
- [ ] Garde-fou : `grep -rnE '<(button|select|textarea|table|dialog|article)\b' internal/web/templates | grep -v 'dl-'` vide ou chaque ligne restante justifiée dans Suivi.
- [ ] Binaire allégé : taille avant/après notée dans Suivi.
- [ ] `make check` vert, aucun `style=` ajouté.
- [ ] Vérifié dans le navigateur à 360 et 1280 px, clair et sombre, et au clavier (focus visible).

## Vérification
```bash
make check
grep -rn 'pico\|--app-' internal/ docs/design-system.md AGENTS.md
```

## Hors scope
Nouveaux composants, theming par cabinet, couleurs praticien/motif (T057).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
- **Clôture (2026-10-16)** : `superseded` — absorbée par T084 (migration daisyUI, voir `docs/decisions.md`). Ne pas l'implémenter.
