# Design system léger

Socle visuel unique : Pico CSS + les jetons `--app-*` de `internal/web/static/app.css` + les partials de `internal/web/templates/partials/`.
Page de contrôle (dev uniquement) : `GET /dev/styleguide` (404 en prod).
Design system v2 « Pétrole & papier » en cours de migration : voir la section en fin de document.

## Règles

- Un message = partial `flash` ; une erreur de formulaire = partial `form_errors`.
- **Aucune couleur hex hors des blocs `:root`** de `app.css`. Pas de nouvelle classe de couleur sans jeton (clair **et** sombre).
- Ni `style=` ni `<script>` inline (CSP) : une classe CSS par variante.
- Vérification : `grep -nE '#[0-9a-fA-F]{3,6}\b' internal/web/static/app.css` ne doit remonter que des lignes de blocs `:root`.

## Jetons

| Famille | Jetons | Usage |
|---|---|---|
| Sémantiques | `--app-{success,error,info,warning}-{bg,fg}` | `.flash-*`, `.form-errors`, `.callout`, jour courant de l'agenda |
| États de RDV | `--app-st-{booked,done,no_show,cancelled}-{bg,fg,border}` | `.cal-appt.st-*`, `.cal-list a.st-*` (`border` = liseré gauche) |
| Agenda | `--app-band-a`, `--app-band-b`, `--app-now` | bande hachurée « indisponible », ligne « maintenant » |
| Espacement | `--app-space-1..4` (0,25 / 0,5 / 0,75 / 1 rem) | marges et paddings des composants |
| Rayon | `--app-radius-sm` (3 px) | RDV de l'agenda |

Le mode sombre redéfinit les mêmes jetons sous `@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) { … } }`, et sous `:root[data-theme="dark"]` (même garde que Pico).
Les pastilles de `/dev/styleguide` sont les classes `.sw-*`.

## Contraste (WCAG 2.x, luminance relative ; seuil 4,5:1 texte, 3:1 non-texte)

Calculé avec un script jetable ; fond de page Pico : `#ffffff` (clair), `#13171f` (sombre).

| Paire | Clair | Sombre |
|---|---|---|
| success fg/bg | `#0b4a1d`/`#e6f4ea` : 9,19 | `#b7e4c4`/`#12301c` : 10,17 |
| error fg/bg | `#7a1010`/`#fdecea` : 9,62 | `#f5c2c0`/`#3d1414` : 10,21 |
| info fg/bg | `#0b2f6b`/`#e8f0fe` : 11,22 | `#c4d8fb`/`#14274d` : 10,20 |
| warning fg/bg | `#5c4400`/`#fff4d6` : 8,40 | `#f5deb0`/`#3d3000` : 9,84 |
| st-booked fg/bg | `#0b2f6b`/`#dbe8ff` : 10,41 | `#c4d8fb`/`#14274d` : 10,20 |
| st-done fg/bg | `#0b4a1d`/`#d9f0df` : 8,69 | `#b7e4c4`/`#12301c` : 10,17 |
| st-no_show fg/bg | `#6b3200`/`#ffe6cc` : 8,38 | `#ffd9b0`/`#3d2300` : 10,98 |
| st-cancelled fg/bg | `#3d3d3d`/`#ececec` : 9,19 | `#cfcfcf`/`#2a2a2a` : 9,21 |
| Liseré st-booked / bg (≥ 3) | 10,41 | 6,09 |
| Liseré st-done / bg | 8,69 | 5,85 |
| Liseré st-no_show / bg | 8,38 | 5,95 |
| Liseré st-cancelled / bg | 4,51 | 4,16 |
| Ligne « maintenant » / page (≥ 3) | `#c0171d` : 6,21 | `#ff6b70` : 6,49 |

Toutes les paires texte/fond dépassent 4,5:1 (minimum 8,38) et tous les éléments non textuels 3:1.

## Partials réutilisables

Appel : `{{template "nom.html" donnée}}` (les partials sont dans le même ensemble que les pages). Réponse htmx : `rd.Partial(w, r, "nom", donnée)`.

| Partial | Donnée attendue | Variante htmx | Exemple |
|---|---|---|---|
| `flash` | `*Flash{Kind, Message}` (`success`, `error`, `info`, `warning`) ; nil = rien | Déjà inclus par les layouts via `.Flash` (cookie signé, `Renderer.SetFlash`) | `{{template "flash.html" .Flash}}` |
| `form_errors` | `[]string` ; vide = rien | Rendu seul avec `rd.Partial(w, r, "form_errors", errs)` | `{{template "form_errors.html" .Data.Errors}}` |
| `patient_fields` | `PatientForm` (noms, contact, opt-in `"1"`, note) | — (champs d'un `<form>` fourni par la page) | `{{template "patient_fields.html" .Data.Form}}` |
| `status_button` | `dict "CSRF" … "D" détail-RDV "View" … "Status" … "Label" …` | `hx-post` → remplace `#appt-detail` | `{{template "status_button.html" (dict "CSRF" .CSRF "D" .D "View" .View "Status" "done" "Label" "Marquer honoré")}}` |
| `patient_results` | `{Patients []{ID, Name, Contact, Selected}, Form.Query}` | Réponse de la recherche de patients (`rd.Partial(…, "patient_results", p)`) | `{{template "patient_results.html" .}}` |
| `booking_summary` | `Summary{Practice, Practitioner, Type, Duration, Start}` | — | `{{template "booking_summary.html" .Data.Summary}}` |
| `booking_hidden`, `waitlist_hidden` | données du parcours de réservation / d'attente | — (champs cachés répétés d'une étape à l'autre) | `{{template "booking_hidden.html" .Data}}` |

Autres partials (`calendar_grid`, `appointment_detail`, `appointment_form`, `availability_panel`, `absences_panel`, `types_panel`, `booking_grid`) : fragments propres à une page, rendus aussi comme réponses htmx de cette page.

## v2 en cours de migration

Le design system v2 (`design/`) remplacera Pico et les jetons `--app-*` famille par famille (tâches T064–T068). Références : [`design/README.md`](../design/README.md) (jetons, composants, règles) et [`design/MIGRATION.md`](../design/MIGRATION.md) (correspondance ancien → nouveau).

Fichiers statiques embarqués (`internal/web/static/`) :

| Fichier | Contenu | Lié ? |
|---|---|---|
| `tokens.css` | copie exacte de `design/tokens.css` (jetons `--dl-*`) | oui |
| `components.css` | `design/components.css` sans le bloc « Base » (sélecteurs d'éléments), plus les classes `.dl-sg-*` du styleguide v2 (jamais dans l'application) | oui |
| `base.css` | le bloc « Base » retiré (reset, titres, liens, focus) | **non**, il remplacera Pico en T068 |

Ordre de chargement dans `layout/base.html` : `pico.min.css` → `tokens.css` → `components.css` → `app.css` (`app.css` garde le dernier mot pendant la transition). Les classes de grille `.gr-N` / `.gs-N` / `.gc-N` restent dans `app.css`.

Coexistence avec Pico : les composants `.dl-*` neutralisent explicitement les styles par élément de Pico (marges de `button` / `input`, `appearance` des champs, puces de liste, fond des `td`, groupe `role="group"`) ; ces corrections vivent dans `design/components.css`, copie source. Une seule exception à « pas de `!important` » : `padding-inline` des champs `[aria-invalid]`, que Pico impose en `!important`.

Page de contrôle : `GET /dev/styleguide/v2` (dev uniquement), chaque bloc en clair puis en sombre (`data-theme="dark"`).
