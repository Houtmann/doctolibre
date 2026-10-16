# Design system : Tailwind v4 + daisyUI 5

Socle visuel : classes utilitaires Tailwind et composants daisyUI, deux thèmes `doctolibre` (clair, défaut) et `doctolibre-dark` définis dans `internal/web/input.css` (palette « pétrole & papier » de `design/tokens.css`).
Le CSS est **généré** dans `internal/web/static/ui.css` (commité) : `make tools` (une fois, télécharge l'exécutable Tailwind épinglé dans `bin/`) puis `make css` après toute modification d'un template, de `static/app.js` ou d'`input.css`. `make css-check` (inclus dans `make check`, obligatoire en CI) vérifie qu'`ui.css` est à jour.
Page de contrôle (dev uniquement) : `GET /dev/styleguide` (404 en prod), composants rendus en clair et en sombre.
Décision et alternatives écartées : `docs/decisions.md` (2026-10-16).

## Règles

- **Aucune couleur hex hors des deux blocs de thème** d'`input.css`. Vérification : `grep -nE '#[0-9a-fA-F]{3,6}\b' internal/web/input.css` ne remonte que des lignes `--color-*` des blocs `@plugin "…daisyui-theme.mjs"`.
- **Aucune classe construite dynamiquement** (`"btn-" + x`) : Tailwind scanne le texte des templates, il faut les noms complets (`{{if …}}alert-error{{else}}…`).
- Ni `style=` ni `<script>` inline, ni image `data:` (CSP `default-src 'self'`). Le grain de daisyUI (`--fx-noise`, image `data:`) est neutralisé dans `input.css`.
- Un message = partial `flash` ; une erreur de formulaire = partial `form_errors`.
- Chaque champ a un `<label>` ; une erreur de champ = `input-error` + `aria-invalid="true"` + texte d'erreur visible (pas la couleur seule).
- Lien d'évitement (`sr-only focus:not-sr-only`) en tête de `base.html`, navigation dans un `<nav aria-label>`, page courante marquée `aria-current="page"`.
- Mobile-first : le menu admin est un `drawer` (bouton « Menu » sous `lg`), les tableaux dans `overflow-x-auto`.

## Thèmes et bascule

- `base.html` ne pose pas `data-theme` : sans JS, `prefers-color-scheme` choisit (daisyUI : thème `doctolibre-dark` est `prefersdark`).
- `static/app.js` (chargé dans `<head>` **sans `defer`**, donc avant le premier rendu : pas de flash) applique le choix mémorisé (`localStorage`, clé `doctolibre-theme`, dans un `try/catch`) ; le bouton `data-theme-toggle` (partial `theme_toggle`) bascule clair/sombre au clic.
- `[data-theme="doctolibre-dark"]` sur un élément suffit à rendre un bloc en sombre (utilisé par le styleguide).

## Jetons

| Famille | Jetons | Usage |
|---|---|---|
| Couleurs daisyUI | `base-100/200/300`, `base-content`, `primary`, `secondary`, `accent`, `neutral`, `info`, `success`, `warning`, `error` (+ `-content`) | classes `bg-*`, `text-*`, `border-*`, composants |
| États de RDV | `--st-{booked,done,noshow,cancelled}-{bg,fg,bd}` (dérivés par `color-mix`) | agenda EventCalendar (T083) ; badges : `badge-soft badge-primary` (réservé), `badge-success` (honoré), `badge-warning` (absent), `badge-ghost` (annulé) |
| EventCalendar | `--ec-*` réaffectés aux jetons daisyUI | CSS de la lib ajouté en T083 |
| Formes | rayons 0,5 / 0,5 / 0,75 rem (sélecteur, champ, boîte), bordure 1 px, sans ombre ni grain | thème |

## Composants retenus (modèles dans `/dev/styleguide`)

| Besoin | Markup |
|---|---|
| Boutons | `btn btn-primary` / `btn-secondary` / `btn-outline` / `btn-ghost` / `btn-error`, `btn-sm`, `disabled` |
| Champ | `<label class="fieldset"><span class="fieldset-legend">…</span><input class="input w-full">` (aide ou erreur : `<span class="label">`) ; `select select`, `textarea textarea`, `checkbox checkbox-primary` |
| Message | `alert alert-success|error|warning|info` (`role="alert"` pour les erreurs, `status` sinon) |
| Tableau | `table` dans `overflow-x-auto rounded-box border`, `<caption class="sr-only">`, `<th scope="col">` |
| Badge d'état | `badge badge-soft badge-primary` etc. (voir ci-dessus) |
| Dialogue | `<dialog class="modal">` + `modal-box` + `modal-action` ; ouverture par `<button data-dialog-open="id">` (`app.js`), fermeture par `<form method="dialog">` |
| Navigation admin | `drawer lg:drawer-open` + `menu` (`menu-active` sur la page courante), `navbar` en haut |

## Contraste (WCAG 2.x, luminance relative ; seuil 4,5:1 texte, 3:1 non-texte)

Calculé le 2026-10-16 avec un script jetable sur les valeurs d'`input.css`.

| Paire (texte / fond) | Clair | Sombre |
|---|---|---|
| base-content / base-100 | 17,22 | 14,10 |
| base-content / base-200 | 15,94 | 15,39 |
| base-content / base-300 | 11,76 | 9,73 |
| primary-content / primary | 7,58 | 8,17 |
| secondary-content / secondary | 9,38 | 7,99 |
| accent-content / accent | 9,77 | 7,61 |
| neutral-content / neutral | 15,94 | 12,17 |
| info-content / info | 6,45 | 7,47 |
| success-content / success | 5,26 | 6,89 |
| warning-content / warning | 5,02 | 6,44 |
| error-content / error | 6,57 | 7,37 |
| primary / base-100 (liens, texte coloré) | 7,58 | 8,07 |
| primary / base-200 | 7,02 | 8,81 |
| error / base-100 | 6,57 | 6,79 |
| success / base-100 | 5,26 | 6,98 |
| warning / base-100 | 5,02 | 6,31 |
| info / base-100 | 6,45 | 7,54 |

Toutes les paires texte/fond dépassent 4,5:1 (minimum 5,02). Les bordures décoratives `base-300` (1,36 à 1,58 contre les fonds) ne portent pas d'information ; en revanche **la bordure des champs** (non-texte, critère 1.4.11) est assombrie dans `input.css` à 55 % de `base-content` (`--input-color`), soit environ 4:1 sur `base-100`, au lieu des 20 % de daisyUI (environ 1,5:1). Les badges « soft » (texte coloré sur teinte de la couleur) et les états EventCalendar sont à re-vérifier avec leurs pages (T081–T083).

## Partials réutilisables

Appel : `{{template "nom.html" donnée}}` (les partials sont dans le même ensemble que les pages). Réponse htmx : `rd.Partial(w, r, "nom", donnée)`.

| Partial | Donnée attendue | Variante htmx | Exemple |
|---|---|---|---|
| `flash` | `*Flash{Kind, Message}` (`success`, `error`, `info`, `warning`) ; nil = rien | Déjà inclus par les layouts via `.Flash` (cookie signé, `Renderer.SetFlash`) | `{{template "flash.html" .Flash}}` |
| `form_errors` | `[]string` ; vide = rien | Rendu seul avec `rd.Partial(w, r, "form_errors", errs)` | `{{template "form_errors.html" .Data.Errors}}` |
| `theme_toggle` | aucune | Bouton de bascule clair/sombre (`data-theme-toggle`), dans les navbars | `{{template "theme_toggle.html"}}` |
| `patient_fields` | `PatientForm` (noms, contact, opt-in `"1"`, note) | — (champs d'un `<form>` fourni par la page) | `{{template "patient_fields.html" .Data.Form}}` |
| `status_button` | `dict "CSRF" … "D" détail-RDV "View" … "Status" … "Label" …` | `hx-post` → remplace `#appt-detail` | `{{template "status_button.html" (dict "CSRF" .CSRF "D" .D "View" .View "Status" "done" "Label" "Marquer honoré")}}` |
| `patient_results` | `{Patients []{ID, Name, Contact, Selected}, Form.Query}` | Réponse de la recherche de patients (`rd.Partial(…, "patient_results", p)`) | `{{template "patient_results.html" .}}` |
| `booking_summary` | `Summary{Practice, Practitioner, Type, Duration, Start}` | — | `{{template "booking_summary.html" .Data.Summary}}` |
| `booking_hidden`, `waitlist_hidden` | données du parcours de réservation / d'attente | — (champs cachés répétés d'une étape à l'autre) | `{{template "booking_hidden.html" .Data}}` |

Autres partials (`calendar_grid`, `appointment_detail`, `appointment_form`, `availability_panel`, `absences_panel`, `types_panel`, `booking_grid`) : fragments propres à une page, rendus aussi comme réponses htmx de cette page.

## Héritage en cours de suppression (T084)

`pico.min.css`, `tokens.css`, `components.css`, `base.css`, `app.css` et la page `/dev/styleguide/v2` restent sur disque mais ne sont **plus liés** par `base.html`. Les pages pas encore migrées (T081–T083) sont donc sommaires jusque-là ; leurs classes `dl-*` et les `partials` ci-dessous qui en dépendent (`patient_fields`, `status_button`, `patient_results`, `booking_*`, `calendar_grid`…) seront réécrits en daisyUI avec leur page. Ne pas ajouter de nouveau lien vers ces fichiers.
