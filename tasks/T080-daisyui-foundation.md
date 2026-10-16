# T080 — Outillage Tailwind + daisyUI, thème, layouts et partials

- **Phase** : 5
- **Dépend de** : — (T062, T064 `done`)
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : **renforcée** (outillage, intégrité du binaire téléchargé, CSP, CI)

## Objectif
Le dépôt sait générer `internal/web/static/ui.css` avec Tailwind CSS v4 (exécutable autonome, sans Node) + daisyUI 5 ; les thèmes `doctolibre` / `doctolibre-dark` existent ; les layouts public et admin et les partials transverses sont en daisyUI ; les conventions du dépôt décrivent la nouvelle pile. Décision : [`docs/decisions.md`](../docs/decisions.md) (2026-10-16, Tailwind v4 + daisyUI 5 + EventCalendar). POC validé par l'humain : `poc/daisyui/`.

## Contexte à lire
`poc/daisyui/{input.css,build.sh,booking.html,agenda.html,app.js}` (référence visuelle et technique, jetable), `design/tokens.css` (palette pétrole, source des valeurs des thèmes), `Makefile`, `.gitignore`, `.github/workflows/` (CI), `internal/web/templates/layout/{base,public,admin}.html`, `partials/{flash,form_errors}.html`, `internal/web/styleguide.go`, `pages/dev_styleguide.html`, `internal/web/render_test.go`, `internal/httpx` (CSP), `AGENTS.md` section Front, `docs/design-system.md`, `PLAN.md` (choix techniques, ligne Front).

## Spécification
- **`make tools`** : télécharge le binaire Tailwind v4 standalone dans `bin/` (gitignoré), OS/arch détectés (`uname`), **version et sha256 épinglés par plateforme** dans le Makefile (ou `tools/tailwind.sha256`) ; échec si la somme ne correspond pas. Idempotent (rien à faire si présent et valide). Versions de départ : celles de `poc/daisyui/build.sh` (Tailwind 4.3.3, daisyUI 5.7.47).
- **daisyUI versionné dans le dépôt** : `tools/daisyui/daisyui.mjs` et `tools/daisyui/daisyui-theme.mjs` (version épinglée notée dans `tools/daisyui/VERSION`, licence MIT conservée).
- **`make css`** : `bin/tailwindcss -i internal/web/input.css -o internal/web/static/ui.css --minify`. **`ui.css` est généré et commité** ; `go build` ne dépend pas du binaire. `make css-check` (inclus dans `make check` si le binaire est présent, obligatoire en CI) : `make css` puis `git diff --exit-code internal/web/static/ui.css`.
- **`internal/web/input.css`** (hors `static/`, non embarqué) : `@import "tailwindcss" source(none);`, `@source` sur `internal/web/templates/**/*.html` et `internal/web/static/*.js`, `@plugin daisyui { themes: false }`, deux `@plugin daisyui-theme` : `doctolibre` (clair, défaut) et `doctolibre-dark` (`prefersdark`), valeurs tirées de `design/tokens.css` (cf. POC). Variables d'état de RDV (`--st-*`) et mapping des variables EventCalendar `--ec-*` vers les jetons daisyUI, comme dans le POC (le CSS de la lib elle-même est ajouté par T083). **Les couleurs ne vivent que dans les blocs de thème** (et les `color-mix` dérivés de jetons) : aucun hex ailleurs.
- **Layouts** : `layout/base.html` (charge `ui.css` et htmx, `lang="fr"`, thème via `data-theme`), `layout/public.html` (navbar simple), `layout/admin.html` (navbar + drawer latéral comme le POC, navigation selon les rôles existants, lien d'évitement, flash). **`base.html` ne lie plus Pico ni l’ancien CSS** (`tokens.css`, `components.css`, `base.css`, `app.css`) : seulement `ui.css`. Les pages pas encore migrées (T081–T084) sont transitoirement sommaires, c’est accepté (décision de l’orchestrateur, 2026-10-16 : éviter que les styles par élément de Pico dégradent daisyUI). Les fichiers restent sur disque jusqu’à T084.
- **Bascule de thème sans script inline** : `static/app.js` (fichier, pas inline) lit/écrit le choix dans `localStorage` (try/catch) et pose `data-theme` ; sans JS, `prefers-color-scheme` décide. Bouton accessible dans la navbar.
- **Partials transverses** en daisyUI : `flash` (`alert`), `form_errors` (`alert alert-error`, `role="alert"`), ainsi que les modèles de champs et boutons du styleguide (`fieldset`/`label`/`input`/`select`/`textarea`/`checkbox`/`btn`), documentés dans `/dev/styleguide`. Les signatures des partials (données attendues, `id`/`hx-target`) ne changent pas.
- **`/dev/styleguide`** réécrit (daisyUI, clair/sombre, boutons, champs, alertes, tableau, badges d'état de RDV, dialogue). Toujours monté seulement en `Env == dev`. `/dev/styleguide/v2` et `dev_styleguide_v2.html` restent jusqu'à T084.
- **Documentation** : `AGENTS.md` section Front (classes utilitaires Tailwind + composants daisyUI ; couleurs uniquement dans les blocs de thème d'`input.css` ; **`make css` après toute modif de template** ; **aucune classe construite dynamiquement** (pas de `"btn-" + x` : écrire les noms complets, sinon Tailwind ne les voit pas) ; pas de style inline ; CSP inchangée), `docs/design-system.md` réécrit (thèmes, composants daisyUI retenus, partials, règles d'accessibilité, ratios AA), `PLAN.md` (ligne Front : « Tailwind v4 standalone + daisyUI 5 », agenda : « EventCalendar » au lieu de « Grille HTML/CSS maison » et « Pico CSS »). Exception au périmètre exécutant : ces trois fichiers sont modifiés par cette tâche.
- **CSP inchangée** : vérifier (test ou navigateur) qu'aucun `style=` ni script inline n'est nécessaire ; si daisyUI ou Tailwind en exige un, le consigner et ne pas assouplir la CSP sans décision.
- Ajouter `bin/` au `.gitignore` s'il n'y est pas.

## Critères d'acceptation
- [x] `make tools && make css` sur une machine neuve produit un `ui.css` identique à celui commité (`git diff --exit-code`).
- [x] Un sha256 erroné fait échouer `make tools` (testé en altérant la somme).
- [x] La CI exécute `make tools` puis la vérification d'`ui.css` à jour.
- [x] `go build ./...` fonctionne sans `bin/tailwindcss`.
- [x] Layouts public et admin rendus en daisyUI, thèmes clair et sombre, bascule sans script inline, vérifiés à 360 et 1280 px.
- [x] `/dev/styleguide` montre les composants en clair et sombre ; ratios de contraste AA vérifiés pour les paires de couleurs des thèmes (outil ou calcul consigné dans Suivi).
- [x] `AGENTS.md`, `docs/design-system.md`, `PLAN.md` à jour.
- [x] `make check` vert, aucun `style=` ni hex hors blocs de thème.

## Vérification
```bash
make tools && make css && git diff --exit-code internal/web/static/ui.css
make check
```

## Hors scope
Migration des pages (T081–T084), EventCalendar (T083), suppression de Pico et de l'ancien CSS (T084), Alpine.js.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : `make tools` (`tools/fetch-tailwind.sh`, sha256 par plateforme dans `tools/tailwind.sha256`, testé avec une somme altérée : échec ; idempotent), `make css`, `make css-check` (compare une génération temporaire à `ui.css`, donc fonctionne même avant le premier commit ; sauté avec message si `bin/tailwindcss` absent, **échoue si `CI` est défini**), CI (`make tools` puis `make css-check` avant `make check`), daisyUI 5.7.47 dans `tools/daisyui/` (+ `VERSION`, `LICENSE` MIT), `internal/web/input.css` + `static/ui.css` généré, layouts base/public/admin (drawer), partials `flash`, `form_errors`, `theme_toggle`, `static/app.js` (thème sans flash + `data-dialog-open`), `/dev/styleguide` réécrit, tests adaptés (`render_test`, `styleguide_test`, `admin_config_test`), `AGENTS.md`, `PLAN.md`, `docs/design-system.md`, `docs/README.md`.
- **Reste à faire** : rien pour T080. Les pages (T081–T083) restent sommaires (CSS Pico/dl-* plus lié) jusqu'à leur migration ; suppression des anciens CSS et du styleguide v2 en T084.
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) :
  - `make tools && make css`. Après toute modif de template : `make css` (sinon `make check` échoue).
  - Contrôle visuel fait au navigateur (serveur de dev sur un port libre, 8099 était pris par le POC) : `/dev/styleguide` à 360 et 1280 px, clair/sombre, bascule, dialogue ; layout admin vérifié en rendant `/admin/settings` d'un test dans une page statique (pas de compte admin créé en base) à 360 et 1280 px, clair et sombre (via `prefers-color-scheme`). Drawer ouvert au clic sur mobile non capturé.
  - Contrastes (script jetable) : toutes les paires texte/fond >= 5,02 ; tableau dans `docs/design-system.md`.
  - `ui.css` contient encore la définition `--fx-noise` (image `data:`) mais `input.css` la remplace par `none` : sans cela, une erreur CSP console par élément.
- **Écarts avec la spécification** (et pourquoi) :
  - `@source` ne scanne que `static/app.js` (pas `static/*.js`) : `htmx.min.js` ajouterait des classes parasites ; T083 ajoutera ses propres fichiers.
  - Bordure des champs assombrie (`--input-color` à 55 % de `base-content`) pour atteindre 3:1 (WCAG 1.4.11), non demandé par la spec.
  - `styleguide.go` : données des anciens blocs (jetons `--app-*`, agenda maison, partials patient/statut/résumé) supprimées ; ces partials ne sont plus montrés dans le styleguide (ils seront migrés avec leurs pages).
  - Lien d'évitement et libellé du nom d'utilisateur : pas de nom d'utilisateur dans la navbar (`View` ne le porte pas).
