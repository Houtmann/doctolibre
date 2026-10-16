# T064 — Design v2 : layouts, navigation, messages, boutons

- **Phase** : 5
- **Dépend de** : T063
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard (+ captures avant/après clair/sombre, 360 et 1280 px)

## Objectif
Toutes les pages partagent le nouvel habillage : en-tête et navigation v2, messages `.dl-alert`, boutons `.dl-btn`. C'est la tâche qui change l'apparence générale ; les familles suivantes (T065–T067) s'appuient dessus.

## Contexte à lire
`design/README.md`, `design/MIGRATION.md`, la ou les cartes `design/previews/` citées ci-dessous, `/dev/styleguide/v2` (T063), `layout/{base,admin,public}.html`, `partials/flash.html`, `partials/form_errors.html`, `partials/status_button.html`, la classe `.callout` et les `role="button"` / `.secondary` / `.outline` dans `pages/` (`grep -rn 'secondary\|outline\|role="button"\|callout' internal/web/templates`). Cartes : `header-nav`, `alerts`, `buttons`.

## Spécification
- Layouts : `.dl-header`, `.dl-brand`, `.dl-nav` avec `aria-current="page"` sur l'onglet courant (le handler expose déjà la page courante ? sinon ajouter un champ au modèle de layout, sans logique métier), `.dl-container` (`--narrow` pour le parcours patient), `.dl-footer`, `.dl-skip-link`.
- `flash` → `.dl-alert.dl-alert--{kind}` ; `form_errors` → `.dl-alert--error` avec `.dl-alert__title` ; `.callout` → `.dl-alert--info`.
- Tous les boutons et liens-boutons des templates → `.dl-btn` + modificateur (`--secondary`, `--outline`, `--ghost`, `--danger` pour annuler/supprimer, `--sm` dans les tableaux et l'agenda). Les boutons **à l'intérieur** des formulaires, du parcours patient et de l'agenda sont inclus ici (seule la classe change) ; leur mise en page reste à T065–T067.
- `.visually-hidden`, `.skip-link`, `.hp`, `.muted`, `.row-actions`, `.inline-actions` → équivalents `.dl-*` (MIGRATION.md).
- Règles communes : remplacer les classes v1 par les classes v2 selon `design/MIGRATION.md` ; les tests `httptest` qui cherchent d'anciennes classes sont mis à jour (fragments v2) ; aucun `style=` ni script inline ; les formulaires marchent toujours sans JS ; les variantes htmx des partials gardent les mêmes `id`/`hx-target`. Si une classe v1 n'est plus utilisée nulle part après la tâche, la retirer d'`app.css` ; sinon la laisser (T068 nettoie).
- Composant manquant ou inadapté : l'ajouter dans `internal/web/static/components.css` **et** `design/components.css` (jetons sémantiques uniquement), et le signaler dans Suivi.

## Critères d'acceptation
- [ ] `grep -rnE 'class="[^"]*\b(flash|form-errors|callout|secondary|outline)\b' internal/web/templates` vide (hors `dev_styleguide.html` v1).
- [ ] Onglet courant marqué `aria-current="page"` dans la navigation admin (test httptest sur une page).
- [ ] `make check` vert, aucun `style=` ajouté.
- [ ] Vérifié dans le navigateur à 360 et 1280 px, clair et sombre, et au clavier (focus visible).

## Vérification
```bash
make check
grep -rnE 'class="[^"]*\b(flash|form-errors|callout|secondary|outline)\b' internal/web/templates
```

## Hors scope
Champs de formulaire et tableaux (T065), parcours patient (T066), agenda et dialogue (T067), retrait de Pico (T068).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
