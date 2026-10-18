# T099 — Agenda : praticiens non chargés au changement de vue

- **Phase** : 5
- **Dépend de** : T083, T057
- **Taille estimée** : S (< 200 lignes)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Dans l'agenda, passer de « Semaine » à « Par praticien » charge les RDV de **tous** les praticiens affichés ; `practitioner=all` en vue jour/semaine charge tous les praticiens et pas seulement le premier.

Origine : bug vu le 2026-10-18 sur une base de démo à 3 praticiens (captures du README) : au clic sur « Par praticien », seul le premier praticien a ses données ; avec `?view=practitioners&practitioner=all` dès le chargement, tout s'affiche.

## Contexte à lire
- `internal/web/static/agenda.js` : `currentView()`, `wantedIds()`, `fetchEvents`, options `eventSources`, `lazyFetching`, `resources`
- `internal/web/calendar.go` (`ResourcesJSON`, `selection`, `picker`), `internal/web/calendar_events.go` (`selectionFrom`), `internal/web/templates/pages/admin_calendar.html`
- `internal/web/calendar_test.go` (`TestCalendarEventsJSON`)

## Spécification
Hypothèse de départ (à confirmer en reproduisant au navigateur, ne pas la croire sur parole) : `wantedIds()` lit `ec.getOption("view")` pendant que EventCalendar déclenche le fetch du nouveau range, avant que l'option `view` soit à jour (ou la source n'est pas rafraîchie au changement de vue) ; donc `wantedIds()` renvoie `[resources[0]]`. Autre piste : `wantedIds()` ignore la sélection `practitioner=…`/`all` de la page, seule la vue décide.
- Corriger la cause : le JS doit connaître la vue **de la requête en cours** (ex. via `info`/`datesSet`/option `viewDidMount`, ou `refetchEvents()` après le changement de vue) et envoyer les `practitioner` correspondant à la sélection de la page (`all` = tous les `resources`).
- Si la cause est aussi côté serveur (sélection non transmise à `ResourcesJSON`, `selectionFrom`), la corriger et ajouter un test de non-régression dans `internal/web/calendar_test.go` (ex. `GET /admin/calendar?view=practitioners&practitioner=all` expose tous les praticiens dans `data-resources` ; `/admin/agenda/events` avec plusieurs `practitioner` renvoie les RDV de chacun).
- `make css` si un template ou `static/app.js` change (pas `agenda.js` seul, mais le lancer ne coûte rien).

## Critères d'acceptation
- [x] Depuis la vue semaine, clic sur « Par praticien » : les colonnes de tous les praticiens affichent leurs RDV (vérifié au navigateur, base de démo à 3 praticiens, via `make run` + `make db-up`).
- [x] Retour à « Semaine » / « Jour » : un seul praticien, ou tous si `practitioner=all`.
- [x] Test de non-régression côté handler si la cause est serveur ; sinon expliquer dans Suivi pourquoi aucun test Go n'est possible.
- [x] `make check` vert.

## Vérification
```bash
make check
```

## Hors scope
Refonte de l'agenda, couleurs (T057), nouvelles vues.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-18 (sonnet)
- **Fait** : cause 100 % côté JS (`internal/web/static/agenda.js`) : « Jour » et « Par praticien » ont la même plage de dates, donc EventCalendar ne relance pas la requête ni `datesSet` au changement de vue entre les deux ; les RDV restent ceux de la requête précédente (un seul praticien après « Jour » ou « Semaine »). Correctif : `viewDidMount` compare les praticiens voulus à ceux de la dernière requête (`fetchedKey`) et appelle `refetchEvents()` ; `wantedIds()` renvoie tous les `resources` si l'URL porte `practitioner=` (sélection explicite, dont `all`) ou en vue « Par praticien », sinon le premier ; `syncView()` reporte aussi la vue dans l'URL et le sélecteur (qui restait périmée entre jour et par praticien).
- **Reste à faire** : rien.
- **Notes de reprise** : vérifié au navigateur (base `doctolibre_demo`, 5 praticiens, port 8095) : semaine → jour → par praticien → semaine → par praticien : 5 `practitioner` à chaque requête utile, 15 RDV par praticien en colonnes ; sans paramètre : jour = 1 praticien, par praticien = 5. Un utilisateur `t099@demo.test` (+ praticien « t099 ») a été créé dans `doctolibre_demo` pour le test.
- **Écarts avec la spécification** : aucun test Go : le serveur (`selectionFrom`, `ResourcesJSON`, `/admin/agenda/events` multi-praticiens) était correct et déjà couvert ; le défaut est une interaction EventCalendar/JS non testable sans navigateur.
