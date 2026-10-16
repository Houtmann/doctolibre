# Doctolibre — design system v2 « Pétrole & papier »

Refonte visuelle du socle actuel (Pico CSS + jetons `--app-*`, cf. `docs/design-system.md`).
Mêmes contraintes : rendu serveur, aucune ressource externe (CSP `default-src 'self'`), pas de `style=` inline dans l'app, contraste AA, mobile-first, clair + sombre.

## Fichiers

| Fichier | Rôle |
|---|---|
| `tokens.css` | Primitives (`--dl-p-*`) et jetons sémantiques (`--dl-*`), clair et sombre |
| `components.css` | Composants autonomes, préfixe `.dl-` — **ne dépend plus de Pico** |
| `grid.css` | Classes de placement de l'agenda `.gr-N` / `.gs-N` / `.gc-N` (CSP) |
| `previews/*.html` | Cartes d'aperçu (clair et sombre côte à côte) |
| `MIGRATION.md` | Correspondance `--app-*` / classes actuelles → v2 |

## Direction

- **Pétrole** (`#0f5e5a`) comme couleur de marque : calme, soignant, distinct du bleu des plateformes commerciales.
- **Papier** : neutres chauds (fond `#f7f6f3`, surfaces blanches) ; en sombre, encre verdâtre (`#121514`).
- « Réservé » prend la couleur de marque : l'agenda d'une journée normale est majoritairement pétrole.
- Formes douces : rayon 8 px (contrôles), 14 px (cartes, dialogue), pilule pour créneaux et badges.
- Police système, chiffres tabulaires (`.dl-num`) pour heures et durées.

## Couches

1. **Primitives** `--dl-p-{petrol,stone,ink}-N` : jamais utilisées directement par un composant.
2. **Sémantiques** : `--dl-{bg,surface,surface-2,border,border-strong,text,text-muted}`, `--dl-primary{,-hover,-soft,-soft-fg}`, `--dl-on-primary`, `--dl-danger`, `--dl-focus`, `--dl-{success,error,info,warning}-{bg,fg}`, `--dl-st-{booked,done,no_show,cancelled}-{bg,fg,border}`, `--dl-band-{a,b}`, `--dl-now`, `--dl-today-bg`.
3. **Échelles** : texte `--dl-text-{xs…3xl}`, espacement `--dl-space-1…8` (base 4 px), rayons `--dl-radius-{sm,md,lg,pill}`, `--dl-target-min` (44 px), ombres `--dl-shadow-{sm,md}`.

Le mode sombre suit le système sauf `data-theme="light"`, et peut être forcé par `data-theme="dark"` (sur `:root` ou sur un conteneur).

## Règles

- Un composant n'utilise **que** des jetons sémantiques ; aucune couleur hex hors de `tokens.css`.
- Un état de RDV n'est jamais porté par la couleur seule : liseré plein (réservé, honoré), pointillé (absent), texte barré (annulé), plus le libellé `.dl-visually-hidden`.
- Toute cible cliquable fait au moins `--dl-target-min`.
- Nouvelle variante = une classe (pas de `style=`), avec ses jetons clair **et** sombre.

## Composants

| Groupe | Classes |
|---|---|
| Base | `.dl-container(--narrow)`, `.dl-stack`, `.dl-cluster`, `.dl-visually-hidden`, `.dl-skip-link`, `.dl-hp`, `.dl-num`, `.dl-muted` |
| Navigation | `.dl-header`, `.dl-brand`, `.dl-nav` (`aria-current="page"`), `.dl-footer` |
| Boutons | `.dl-btn` + `--secondary`, `--outline`, `--ghost`, `--danger`, `--sm`, `--block` ; `.dl-segmented` |
| Formulaires | `.dl-field` (`.is-invalid`), `.dl-label`, `.dl-input`, `.dl-select`, `.dl-textarea`, `.dl-help`, `.dl-check`, `.dl-fieldset`, `.dl-note-warning` |
| Messages | `.dl-alert--{success,error,info,warning}`, `.dl-alert__title` (remplace `flash`, `form-errors`, `callout`) |
| Badges | `.dl-badge`, `--brand`, `--st-{booked,done,no_show,cancelled}` |
| Cartes | `.dl-card`, `__header`, `__footer` ; `.dl-details` (dl) |
| Parcours patient | `.dl-steps`, `.dl-choices` / `.dl-choice`, `.dl-week-nav`, `.dl-slot-day`, `.dl-slots` / `.dl-slot`, `.dl-empty` |
| Tableaux | `.dl-table-wrap`, `.dl-table` |
| Dialogue | `.dl-dialog`, `__header`, `__body`, `__footer` |
| Agenda | `.dl-cal-toolbar`, `.dl-cal.cols-N`, `__head(.is-today)`, `__hour`, `__hour-label`, `__col`, `__band`, `__slot`, `__now`, `.dl-appt.st-*`, `.dl-cal-list` |

## Contraste (WCAG 2.x)

| Paire | Clair | Sombre |
|---|---|---|
| Texte / fond | 15,9 | 15,4 |
| Texte atténué / surface | 7,1 | 7,3 |
| Primaire / surface | 7,6 | 8,1 |
| Texte sur primaire | 7,6 | 8,2 |
| Messages fg/bg (min) | 7,9 | 10,4 |
| États RDV fg/bg (min) | 7,8 | 8,4 |
| Liserés d'état / fond (min, ≥ 3) | 3,1 | 3,8 |
| Bordure de champ / surface (≥ 3) | 3,7 | 3,4 |
| Ligne « maintenant » / surface | 5,9 | 6,6 |
