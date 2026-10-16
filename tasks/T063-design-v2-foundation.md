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
- [x] `tokens.css` identique à `design/tokens.css` ; `components.css` = `design/components.css` moins le bloc Base (`diff` commenté dans Suivi).
- [x] Rendu inchangé des pages existantes (agenda semaine, réservation patient, une page avec flash) : captures avant/après par l'orchestrateur.
- [x] `/dev/styleguide/v2` : 200 en dev, 404 en prod (test `httptest`) ; contient `dl-btn`, `dl-alert--error`, `dl-appt st-no_show`, `data-theme="dark"`.
- [x] Aucun `style=` ni script inline ajouté ; `grep -nE '#[0-9a-fA-F]{3,6}\b' internal/web/static/components.css` vide.
- [x] Les composants de `/dev/styleguide/v2` ressemblent aux aperçus `design/previews/` (pas de fuite Pico visible : boutons pleine largeur, marges de formulaire, fond de tableau…).

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

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : `static/{tokens,components,base}.css` ; `layout/base.html` (pico → tokens → components → app) ; page `/dev/styleguide/v2` (dev seulement, template `dev_styleguide_v2.html`, données factices `styleguideV2View()`), lien depuis `/dev/styleguide`, cas ajouté dans `render_test.go`, tests httptest 200 dev / 404 prod ; section « v2 en cours de migration » dans `docs/design-system.md`. Fuites Pico corrigées dans `design/components.css` puis reportées (voir ci-dessous).
- **Reste à faire** : captures avant/après des pages existantes par l'orchestrateur (critère 2 non coché) ; contrôle visuel navigateur.
- **Notes de reprise** :
  - `design/components.css` a été restructuré : le bloc Base est encadré par `/* BASE-START … */` / `/* BASE-END */` (il contient aussi la règle `@media (prefers-reduced-motion) { * {…} }` qui était en fin de fichier, et `small` ; `.dl-small` est sorti du bloc). `static/base.css` = ce bloc ; `static/components.css` = le reste de `design/components.css` + le bloc `.dl-sg-*` (styleguide, uniquement dans la copie statique). Vérif : `diff` entre `design/components.css` sans bloc Base et `static/components.css` ne montre que une ligne vide et le bloc `.dl-sg-*` final.
  - `tokens.css` : `color-scheme: light dark` sur `:root` est écrasé par Pico (`:root:not([data-theme=dark])` plus spécifique), donc aucun changement sur les pages existantes.
  - Fuites Pico neutralisées dans `design/components.css` : `margin:0`/`box-shadow:none` sur `.dl-btn` et `.dl-segmented` (+ `role="group"` de Pico), sélecteurs `input.dl-input`/`select.dl-select`/`textarea.dl-textarea` qualifiés par l'élément (égalité de spécificité avec Pico, on gagne par l'ordre), `appearance:auto` du select, `.dl-check input[type]{all:revert}` (case Pico bleue), `.dl-fieldset{background:none}`, `.dl-table{margin:0}` + `td{background:transparent}`, puces/marges des `li` des listes `.dl-*`, `.dl-label{margin:0}`.
  - Seul `!important` : `padding-inline` des champs `[aria-invalid]` (Pico le met en `!important` pour l'icône d'erreur) — impossible autrement.
  - Les panneaux « clair » du styleguide portent `data-theme="light"` mais tokens.css ne redéfinit pas les jetons pour `[data-theme=light]` en dehors de `:root` : si l'OS est en mode sombre, ils s'affichent en sombre (comportement des jetons, non modifiable ici).
  - Dans le styleguide v2, le texte non classé (ul, li, td…) hérite encore de la couleur Pico (le `body` v2 n'est pas lié) ; sans conséquence tant que `base.css` n'est pas lié (T068).
  - Vérif navigateur : `DOCTOLIBRE_ENV=dev DOCTOLIBRE_HTTP_ADDR=:8099 DOCTOLIBRE_BASE_URL=http://localhost:8099 DOCTOLIBRE_SECRET_KEY=dev-only-secret-key-do-not-use-in-prod-0123456789 DOCTOLIBRE_DATABASE_URL='postgres://hadrien@127.0.0.1:5432/doctolibre_test?sslmode=disable' go run ./cmd/doctolibre serve` puis http://localhost:8099/dev/styleguide/v2 (le hash des assets est calculé au démarrage : relancer après modif CSS).
- **Écarts avec la spécification** : `design/components.css` modifié aussi pour déplacer `small` / la règle reduced-motion `*` dans le bloc Base (sinon sélecteurs nus dans le fichier lié) ; `.dl-segmented` sans `role="group"` conseillé (Pico) mais neutralisé ; `<dialog open>` rendu statique via `.dl-sg-panel .dl-dialog`.
- **Contrôle orchestrateur (2026-10-16)** : `make check` relancé ✅ ; diff `tokens.css` vide ; aucun sélecteur nu dans les CSS liés. Rendu inchangé vérifié par comparaison des styles calculés (toutes propriétés non personnalisées de chaque élément) avec et sans `tokens.css`/`components.css`, sur `/dev/styleguide` v1 (agenda, flash, formulaires, partials) et `/`, en clair et en sombre : 0 écart. `/dev/styleguide/v2` vu à 1280 px : conforme aux aperçus, pas de débordement horizontal. Reste ouvert (à traiter en T064) : un conteneur `data-theme="light"` dans un système sombre reste sombre, `tokens.css` ne redéclare pas les valeurs claires hors `:root`.
