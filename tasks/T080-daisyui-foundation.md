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
- [ ] `make tools && make css` sur une machine neuve produit un `ui.css` identique à celui commité (`git diff --exit-code`).
- [ ] Un sha256 erroné fait échouer `make tools` (testé en altérant la somme).
- [ ] La CI exécute `make tools` puis la vérification d'`ui.css` à jour.
- [ ] `go build ./...` fonctionne sans `bin/tailwindcss`.
- [ ] Layouts public et admin rendus en daisyUI, thèmes clair et sombre, bascule sans script inline, vérifiés à 360 et 1280 px.
- [ ] `/dev/styleguide` montre les composants en clair et sombre ; ratios de contraste AA vérifiés pour les paires de couleurs des thèmes (outil ou calcul consigné dans Suivi).
- [ ] `AGENTS.md`, `docs/design-system.md`, `PLAN.md` à jour.
- [ ] `make check` vert, aucun `style=` ni hex hors blocs de thème.

## Vérification
```bash
make tools && make css && git diff --exit-code internal/web/static/ui.css
make check
```

## Hors scope
Migration des pages (T081–T084), EventCalendar (T083), suppression de Pico et de l'ancien CSS (T084), Alpine.js.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
