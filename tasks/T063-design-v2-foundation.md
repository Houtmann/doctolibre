# T063 — Design system v2 : socle embarqué et styleguide v2

- **Phase** : 5
- **Dépend de** : T062
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard (+ contrôle navigateur par l'orchestrateur)

## Objectif
Les fichiers du design system v2 « Pétrole & papier » (`design/`) sont servis par l'application à côté de Pico, **sans aucun changement visuel sur les pages existantes**, et une page `/dev/styleguide/v2` montre chaque composant `.dl-*` rendu par l'app (clair et sombre). Les tâches T064–T067 peuvent ensuite migrer les templates famille par famille.

## Contexte à lire
`design/README.md`, `design/MIGRATION.md`, `design/tokens.css`, `design/components.css`, `design/previews/*.html` (modèles visuels à reproduire, **ne pas** copier `_preview.css` tel quel), `internal/web/templates/layout/base.html`, `internal/web/styleguide.go`, `internal/web/styleguide_test.go`, `internal/web/templates/pages/dev_styleguide.html`, `internal/web/routes.go` (l. ~75–85), `internal/web/render_test.go`, `docs/design-system.md`.

## Spécification
- **Fichiers statiques** (dans `internal/web/static/`, embarqués comme les autres) :
  - `tokens.css` : copie de `design/tokens.css`.
  - `components.css` : copie de `design/components.css` **sans** le bloc « Base » des sélecteurs d'éléments (`*`, `html`, `body`, `h1…h4`, `p/ul/ol/dl`, `small`, `a`, `code/kbd`, `hr`, et la règle `:where(...):focus-visible`). Ce bloc va dans `base.css`.
  - `base.css` : le bloc retiré ci-dessus, **non lié** dans le layout pour l'instant (il remplacera Pico en T068). Commentaire d'en-tête qui le dit.
  - **Pas** de `grid.css` : les classes `.gr-N`/`.gs-N`/`.gc-N` d'`app.css` sont identiques à `design/grid.css` (vérifié) ; elles y restent jusqu'à T068.
- **Layout** `layout/base.html` : ordre `pico.min.css` → `tokens.css` → `components.css` → `app.css` (app.css garde le dernier mot pendant la transition).
- **Coexistence avec Pico** : les classes `.dl-*` doivent rendre comme les aperçus de `design/previews/` malgré Pico (qui stylise `button`, `input`, `select`, `table`, `dialog`, `article`, `[role=button]`… par élément). Si Pico fuit sur un composant (marges, largeur 100 %, padding, fond, `--pico-*`), corriger **dans `components.css`** en neutralisant explicitement la propriété sur la classe `.dl-*` (pas de `!important` sauf impossibilité, à justifier dans Suivi). Reporter les mêmes corrections dans `design/components.css` pour garder les deux copies identiques (hors bloc Base).
- **Aucun** changement visuel ailleurs : aucune classe `.dl-*` n'est encore utilisée dans les templates applicatifs ; aucun sélecteur d'élément nu dans `tokens.css`/`components.css` liés (sauf `:root` / `[data-theme]`).
- **Styleguide v2** : `GET /dev/styleguide/v2`, monté uniquement si `Env == EnvDev` (404 en prod, testé), handler dans `styleguide.go`, template `pages/dev_styleguide_v2.html`, layout public. Contenu : un bloc par carte de `design/previews/` (couleurs sémantiques, états de RDV, typographie, boutons, formulaires avec `.dl-field.is-invalid`, alertes, badges, carte récapitulatif, parcours de réservation, agenda semaine `.dl-cal` avec un RDV par état, liste mobile `.dl-cal-list`, en-tête/nav, tableau, dialogue ouvert via `<dialog open>`). Chaque bloc est rendu **deux fois** : une fois tel quel, une fois dans un conteneur `data-theme="dark"`. Les classes de mise en page propres à cette page (grille deux colonnes, panneau) vont dans `components.css` sous un préfixe `.dl-sg-` documenté « styleguide uniquement » — pas de `style=`. Données factices construites en Go (pas de DB), comme pour `/dev/styleguide`.
- Lien vers `/dev/styleguide/v2` depuis `/dev/styleguide`.
- `docs/design-system.md` : courte section « v2 en cours de migration » qui renvoie à `design/README.md` et `design/MIGRATION.md`, liste les fichiers statiques et l'ordre de chargement.

## Critères d'acceptation
- [ ] `tokens.css` identique à `design/tokens.css` ; `components.css` = `design/components.css` moins le bloc Base (`diff` commenté dans Suivi).
- [ ] Rendu inchangé des pages existantes (agenda semaine, réservation patient, une page avec flash) : captures avant/après par l'orchestrateur.
- [ ] `/dev/styleguide/v2` : 200 en dev, 404 en prod (test `httptest`) ; contient `dl-btn`, `dl-alert--error`, `dl-appt st-no_show`, `data-theme="dark"`.
- [ ] Aucun `style=` ni script inline ajouté ; `grep -nE '#[0-9a-fA-F]{3,6}\b' internal/web/static/components.css` vide.
- [ ] Les composants de `/dev/styleguide/v2` ressemblent aux aperçus `design/previews/` (pas de fuite Pico visible : boutons pleine largeur, marges de formulaire, fond de tableau…).

## Vérification
```bash
make check
diff design/tokens.css internal/web/static/tokens.css
grep -nE '#[0-9a-fA-F]{3,6}\b' internal/web/static/components.css
grep -rn 'style=' internal/web/templates | grep -v '^.*{{/\*'
```

## Hors scope
Migrer un template applicatif vers `.dl-*` (T064–T067), retirer Pico ou `app.css` (T068), déplacer les classes de grille, modifier les jetons (les valeurs sont validées pour le contraste AA).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
