# T067 — Design v2 : agenda et dialogue de RDV

- **Phase** : 5
- **Dépend de** : T064
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (+ captures avant/après clair/sombre, 360 et 1280 px)

## Objectif
L'agenda praticien (vues jour et semaine, multi-praticiens, liste mobile) et le dialogue de RDV suivent les cartes `calendar-week`, `calendar-list-mobile` et `dialog` ; l'état d'un RDV n'est jamais porté par la couleur seule.

## Contexte à lire
`design/README.md`, `design/MIGRATION.md`, la ou les cartes `design/previews/` citées ci-dessous, `/dev/styleguide/v2` (T063), `pages/admin_calendar.html`, `pages/admin_appointment.html`, `partials/{calendar_grid,appointment_detail,status_button}.html`, `internal/web/static/app.js` (ouverture du dialogue, si des sélecteurs de classe y figurent), `app.css` section Agenda (T016), tests de l'agenda (dont `TestCalendarWeekWith150AppointmentsIsFast`). Cartes : `calendar-week`, `calendar-list-mobile`, `dialog`, `colors-appointment-states`.

## Spécification
- `.cal-grid.cal-cols-N` → `.dl-cal.cols-N` ; `.cal-head(.is-today)`, `.cal-hour`, `.cal-hour-label`, `.cal-band`, `.cal-slot`, `.cal-now` → `.dl-cal__*` ; `.cal-appt.st-*` → `.dl-appt.st-*` avec libellé d'état `.dl-visually-hidden` ; `.cal-list` → `.dl-cal-list` ; `.cal-toolbar` → `.dl-cal-toolbar` + `.dl-segmented` Jour/Semaine (`aria-current`).
- Les classes de placement `.gr-N`/`.gs-N`/`.gc-N` ne changent pas (déjà générées dans `app.css`).
- Dialogue `#appt-dialog` → `dialog.dl-dialog` + `__header/__body/__footer` ; boutons d'état en `.dl-btn--sm`. Garder l'id et le comportement htmx/JS.
- Vue jour à 4 praticiens lisible à 1280 px (critère T033 jamais coché).
- Règles communes : remplacer les classes v1 par les classes v2 selon `design/MIGRATION.md` ; les tests `httptest` qui cherchent d'anciennes classes sont mis à jour (fragments v2) ; aucun `style=` ni script inline ; les formulaires marchent toujours sans JS ; les variantes htmx des partials gardent les mêmes `id`/`hx-target`. Si une classe v1 n'est plus utilisée nulle part après la tâche, la retirer d'`app.css` ; sinon la laisser (T068 nettoie).
- Composant manquant ou inadapté : l'ajouter dans `internal/web/static/components.css` **et** `design/components.css` (jetons sémantiques uniquement), et le signaler dans Suivi.

## Critères d'acceptation
- [ ] `grep -rn 'cal-' internal/web/templates internal/web/static/app.js` ne remonte que `dl-cal` (classes v2).
- [ ] Chaque RDV porte un libellé d'état accessible (test httptest sur les 4 états, dont annulé si affiché).
- [ ] Test de performance de l'agenda toujours vert.
- [ ] `make check` vert, aucun `style=` ajouté.
- [ ] Vérifié dans le navigateur à 360 et 1280 px, clair et sombre, et au clavier (focus visible).

## Vérification
```bash
make check
grep -rn 'cal-' internal/web/templates internal/web/static/app.js | grep -v 'dl-cal'
```

## Hors scope
Couleurs praticien/motif (T057), retrait de Pico (T068), toute modification du calcul des positions.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
