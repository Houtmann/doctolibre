# T084 — Pages restantes, retrait de Pico et de l'ancien CSS

- **Phase** : 5
- **Dépend de** : T081, T082, T083
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard (+ passage de contrôle complet, captures 360 et 1280 px, clair et sombre)

## Objectif
Plus aucune page ne dépend de Pico ni du CSS maison : `ui.css` généré est la seule feuille de style applicative. Les dossiers `design/` et `poc/` ne sont plus nécessaires. Absorbe T068.

## Contexte à lire
T080–T083 (Suivi), `internal/web/static/`, `internal/web/styleguide.go`, `internal/web/render_test.go`, `internal/web/templates/`, `AGENTS.md`, `docs/design-system.md`, `design/README.md`, `design/MIGRATION.md`, `poc/daisyui/`, `docs/decisions.md`.

## Spécification
Templates **de cette tâche** (et eux seuls) : `pages/login.html`, `password_forgot.html`, `password_reset.html`, `invitation.html`, `admin_dashboard.html`, `admin_status.html`, `error.html` (contrôle de la variante admin ; le rendu public est fait en T081), et le retrait de `pages/dev_styleguide_v2.html`.

- Migrer ces pages en daisyUI (mêmes règles que T081/T082 : formulaires sans JS, champs en erreur marqués, `id`/`hx-target` conservés).
- **Garde-fou avant suppression** : lister les éléments que Pico stylisait par élément (`button`, `a[role=button]`, `input`, `select`, `textarea`, `table`, `dialog`, `article`, `nav ul`, `details`, `progress`) encore présents sans classe daisyUI ; migrer chaque occurrence. Liste et traitement dans Suivi.
- **Supprimer** : `static/pico.min.css`, `tokens.css`, `components.css`, `base.css`, `grid.css` éventuel, `app.css` écrit à la main, leurs liens dans les layouts, les partials/classes `dl-*` inutilisés, les styles `.dl-sg-*`, la route et le test `/dev/styleguide/v2`. Corriger la CSP si elle ne devait ses assouplissements qu'à Pico (icônes `data:`).
- **`design/` et `poc/`** : supprimer (l'historique git les conserve) ou archiver hors du dépôt ; si une valeur de `design/` reste utile, elle est déjà dans les thèmes d'`input.css`. Mettre à jour `docs/README.md` et tout lien vers ces dossiers.
- Documentation : retirer de `docs/design-system.md`, `AGENTS.md`, `docs/progress/` (hors journal) les références à Pico et à `--app-*`/`--dl-*` ; ajouter une ligne à `docs/decisions.md` si un choix imprévu est fait.
- **Passage de contrôle complet** (consigné dans Suivi, page par page : liste de `render_test.go` + agenda) : 360 px et 1280 px, clair et sombre, contrastes AA, navigation au clavier (ordre, focus visible, dialogue : piège de focus et `Échap`), pas de débordement horizontal, aucune erreur de console ni violation CSP.
- Taille du binaire avant/après notée dans Suivi.

## Critères d'acceptation
- [ ] `grep -rniE 'pico|--app-|--dl-|dl-' internal/ docs/design-system.md AGENTS.md PLAN.md` vide (hors `decisions.md`/journal).
- [ ] `grep -rnE '<(button|select|textarea|table|dialog|article)\b' internal/web/templates | grep -v 'class='` vide ou chaque ligne justifiée dans Suivi.
- [ ] Les fichiers CSS hérités et `design/`, `poc/` sont supprimés ; `internal/web/static/` ne contient plus que `ui.css` (généré), `app.js`, `agenda.js`, `htmx.min.js`, `vendor/`.
- [ ] Toutes les pages vérifiées (contrôle ci-dessus) ; défauts d'affichage repérés dans STATUS.md levés ou notés.
- [ ] `make css` sans diff ; `make check` vert, aucun `style=`.

## Vérification
```bash
make css && git diff --exit-code internal/web/static/ui.css
make check
grep -rniE 'pico|--app-|--dl-' internal/ docs/design-system.md AGENTS.md PLAN.md
```

## Hors scope
Nouvelles fonctionnalités et composants, theming par cabinet, couleurs praticien/motif (T057).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
