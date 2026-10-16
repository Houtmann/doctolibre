# Migration v1 (Pico + `--app-*`) → v2 (`--dl-*`)

## Jetons

| v1 | v2 |
|---|---|
| `--app-{success,error,info,warning}-{bg,fg}` | `--dl-{success,error,info,warning}-{bg,fg}` |
| `--app-st-{state}-{bg,fg,border}` | `--dl-st-{state}-{bg,fg,border}` |
| `--app-band-a`, `--app-band-b`, `--app-now` | `--dl-band-a`, `--dl-band-b`, `--dl-now` |
| `--app-space-1..4` | `--dl-space-1..4` (échelle étendue jusqu'à 8) |
| `--app-radius-sm` (3 px) | `--dl-radius-sm` (4 px) |
| `--pico-primary` | `--dl-primary` |
| `--pico-muted-color` | `--dl-text-muted` |
| `--pico-muted-border-color` | `--dl-border` |
| `--pico-background-color` | `--dl-bg` / `--dl-surface` |
| `--pico-border-radius` | `--dl-radius-md` |

## Classes

| v1 | v2 |
|---|---|
| `.flash.flash-{kind}` | `.dl-alert.dl-alert--{kind}` |
| `.form-errors` | `.dl-alert.dl-alert--error` |
| `.callout` | `.dl-alert.dl-alert--info` |
| `button` (Pico) / `a[role=button]` | `.dl-btn` |
| `.secondary` | `.dl-btn--secondary` |
| `.outline` / `.secondary.outline` | `.dl-btn--outline` |
| `.choice-list a[role=button]` | `.dl-choices` + `.dl-choice` |
| `.slot-list a[role=button]` | `.dl-slots` + `.dl-slot` |
| `.week-nav` | `.dl-week-nav` |
| `.booking-summary` | `.dl-card` + `.dl-details` |
| `.detail-list` | `.dl-details` |
| `dialog#appt-dialog` + `article` | `dialog.dl-dialog` + `__header/__body/__footer` |
| `.cal-grid.cal-cols-N` | `.dl-cal.cols-N` |
| `.cal-head(.is-today)`, `.cal-hour`, `.cal-hour-label`, `.cal-band`, `.cal-slot`, `.cal-now` | `.dl-cal__head`, `__hour`, `__hour-label`, `__band`, `__slot`, `__now` |
| `.cal-appt.st-*` | `.dl-appt.st-*` |
| `.cal-list` | `.dl-cal-list` |
| `.cal-toolbar` | `.dl-cal-toolbar` (+ `.dl-segmented` pour Jour/Semaine) |
| `.row-actions`, `.inline-actions` | `.dl-cluster` |
| `.visually-hidden`, `.skip-link`, `.hp`, `.muted` | `.dl-visually-hidden`, `.dl-skip-link`, `.dl-hp`, `.dl-muted` |
| `.home-badge` | `.dl-home` ou `.dl-badge` « ⌂ Domicile » |
| `.sw-*` (styleguide) | remplacées par `previews/` |

## Nouveautés sans équivalent v1

`.dl-header`/`.dl-nav` (onglet courant), `.dl-steps`, `.dl-badge--st-*`, `.dl-table`, `.dl-empty`, `.dl-segmented`, `.dl-field.is-invalid`, `.dl-note-warning` (avertissement « note administrative »), `.dl-cal__col`.

## Ordre conseillé

1. Embarquer `tokens.css`, `components.css`, `grid.css` dans `internal/web/static` à côté de Pico.
2. Migrer partials puis pages, une famille à la fois (messages → boutons → formulaires → parcours patient → agenda).
3. Retirer `pico.min.css` et `app.css` une fois `grep -rn 'class="\(secondary\|outline\|flash\|cal-\)' internal/web/templates` vide.
4. Mettre à jour `/dev/styleguide` et `docs/design-system.md`.
