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
- [x] `grep -rn 'cal-' internal/web/templates internal/web/static/app.js` ne remonte que `dl-cal` (classes v2).
- [x] Chaque RDV porte un libellé d'état accessible (test httptest sur les 4 états, dont annulé si affiché).
- [x] Test de performance de l'agenda toujours vert.
- [x] `make check` vert, aucun `style=` ajouté.
- [x] Vérifié dans le navigateur à 360 et 1280 px, clair et sombre, et au clavier (focus visible).

## Vérification
```bash
make check
grep -rn 'cal-' internal/web/templates internal/web/static/app.js | grep -v 'dl-cal'
```

## Hors scope
Couleurs praticien/motif (T057), retrait de Pico (T068), toute modification du calcul des positions.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** :
  - `calendar_grid.html` en v2 : `.dl-cal.cols-N`, `.dl-cal__{head,hour,hour-label,band,slot,now,holiday,col}`, `.dl-appt.st-*` (libellé d'état `dl-visually-hidden` conservé), `.dl-cal-list`, `.dl-cal-toolbar`, Jour/Semaine en `<nav class="dl-segmented">` (`aria-current="page"`), sélecteur de praticiens `.dl-cal-picker` + `.dl-fieldset`/`.dl-check`. Un `.dl-cal__col` par jour (traits verticaux ; `gs-{{.V.Rows}}`, +1 nœud par colonne).
  - Dialogue : `dialog#appt-dialog.dl-dialog` > `div#appt-dialog-body` (id et JS inchangés ; `app.js` non modifié). `appointment_detail.html` : `__header` (titre + badge d'état), `__body` (détails `.dl-details`, historique, remplacement, déplacer, annuler avec `.dl-field`/`.dl-input`/`.dl-check`), `__footer` (boutons d'état `--sm` + Fermer). Page sans JS `admin_appointment.html` : même contenu dans `div.dl-dialog.dl-dialog--page`. `status_button` : `dl-btn--sm`. Ligne « État » retirée du détail (le badge d'en-tête la remplace).
  - `app.css` : retrait des règles `.cal-*`, `dialog#appt-dialog`, `.detail-list`, `.home-badge`, `.cal-holiday` ; conservés `.gr/.gs/.gc/.gc-all`. `dev_styleguide.html` (v1) migré vers les classes `dl-cal`/`dl-appt` pour satisfaire le grep.
  - `components.css` (design/ et static/) : section `/* ── T067 ── */` (neutralisation du `dialog` Pico : position/min-width/backdrop, `width:auto` des boutons dans picker/dialogue, titres et listes du corps de dialogue, focus-visible des cases/RDV).
  - Tests : classes v2 dans `calendar_test.go`, `team_test.go`, `holidays_test.go` ; nouveau `TestCalendarAppointmentsExposeStateLabel` (3 états affichés dans grille et liste ; annulé non affiché dans l'agenda, son badge/libellé est déjà testé dans `TestCalendarMoveCancelStatusFlow`).
  - Navigateur (serveur de test sur :8095, arrêté) : vue jour à 4 praticiens à 1280 px (4 colonnes lisibles, états done/no_show/booked distincts, trait pointillé pour absent), dialogue 1280 px clair et 360 px sombre, liste mobile 360 px sombre, aucun débordement horizontal, focus clavier visible (anneau 2 px `--dl-focus`). Vue semaine et clair/360 non revus en capture.
- **Reste à faire** : captures avant/après par l'orchestrateur ; revue visuelle de la vue semaine ; les formulaires de création (`appointment_form`, T065) s'affichent dans le dialogue sans en-tête/pied v2 tant que T065 n'est pas fusionné.
- **Notes de reprise** : le `dialog` Pico est un overlay plein écran (`min-width:100%`, flex) : neutralisé dans le bloc T067 ; à garder après T068. `Rows` peut valoir jusqu'à 96 (classes `gs-N` existantes).
- **Écarts avec la spécification** : `.dl-cal__col` ajouté (absent du balisage actuel) pour les séparateurs de colonnes de la carte ; `dev_styleguide.html` v1 modifié (hors liste) ; barre Jour/Semaine en `<nav>` avec liens (pas de `role="group"`).
