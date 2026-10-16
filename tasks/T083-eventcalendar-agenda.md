# T083 — Agenda staff avec EventCalendar

- **Phase** : 5
- **Dépend de** : T080
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : **renforcée** (requête sur `appointment`, isolation par cabinet, fuseau et changement d'heure, CSP)

## Objectif
L'agenda du personnel est rendu par EventCalendar (vkurko, `@event-calendar/build`) alimenté par un endpoint JSON ; un clic sur un RDV ouvre un dialogue daisyUI avec les actions existantes. Sans JS, la liste du jour rendue serveur reste utilisable.

## Contexte à lire
`poc/daisyui/{agenda.html,agenda.js,vendor/}` (référence : vues, ressources, mapping `--ec-*` déjà dans `input.css`), T080 (Suivi), T016 (agenda existant) et T067 (Suivi), `internal/web/calendar.go`, `internal/service/calendar.go` (`View`, `SetStatus`, `Detail`, `ViewablePractitioners`), `internal/web/access.go`, `internal/web/routes.go`, `internal/web/templates/pages/admin_calendar.html` et partials, `internal/web/calendar_test.go` (dont `TestCalendarWeekWith150AppointmentsIsFast`), `docs/decisions.md` (agenda : classes `.gr/.gs/.gc`, buffers, `during_blocked`), `AGENTS.md` (SQL : EXPLAIN, `practice_id`).

## Spécification
Templates **de cette tâche** (et eux seuls) : `pages/admin_calendar.html`, `pages/admin_appointment.html` (page plein écran d'un RDV, repli sans JS et cible du dialogue), `partials/calendar_grid.html` (remplacé par le repli « liste du jour »), `partials/appointment_detail.html`, `partials/status_button.html`, `pages/admin_appointment_new.html` et `partials/appointment_form.html` (formulaire de création de RDV : doit fonctionner en page plein écran sans JS et dans le `<dialog>`).

- **Endpoint JSON** `GET /admin/agenda/events?start=&end=&practitioner=` (authentifié, droits via `domain.MayAccessPractitioner` comme l'agenda actuel) :
  - filtre `practice_id` pris dans la **session**, jamais dans la requête ; `practitioner` limité aux agendas visibles (plusieurs valeurs permises) ;
  - `start`/`end` ISO 8601, plage bornée à **42 jours maximum** (400 sinon), `end > start` ;
  - réponse : événements `{id, resourceId, start, end, title, extendedProps{status, blocked_start, blocked_end, type, home, …}}` avec `during` (le soin) et `during_blocked` (buffers inclus, pour dessiner les marges) ; **sans donnée superflue** (pas de téléphone, email ni note ; titre minimal) ; statuts `booked/done/no_show/cancelled` ;
  - indisponibilités (hors horaires, absences, jours fériés) en événements d'arrière-plan (`display: background`) ;
  - heures sérialisées avec le décalage du fuseau du cabinet (`practice.timezone`).
- **Page agenda** : charge EventCalendar **uniquement ici**, depuis `internal/web/static/vendor/` (fichiers embarqués, version épinglée notée dans `vendor/VERSION`, licence conservée) ; `static/agenda.js` (fichier, pas inline) configure vues **semaine / jour / par praticien** (ressources), lit l'endpoint, navigation et choix du praticien en conservant l'URL ; couleurs par statut via les classes `appt-*` et les jetons `--st-*` d'`input.css`.
- **Dialogue** : clic sur un RDV → `<dialog>` daisyUI rempli en htmx par le partial serveur `appointment_detail` (actions existantes : honoré, absent, annuler, déplacer ; `status_button`), puis rafraîchissement de l'agenda (événement htmx déclenchant `refetchEvents`). Nouveau RDV : même dialogue avec `appointment_form`.
- **Repli sans JS** : `<noscript>` ou rendu par défaut remplacé par JS : liste du jour (praticien, heure, patient, motif, statut, lien vers la page du RDV) rendue côté serveur, navigation jour précédent/suivant.
- **Hors périmètre v1** : glisser-déposer / redimensionnement (le déplacement passe par le formulaire existant).
- **Suppression** de la grille maison (`calendar_grid`, classes `.gr/.gs/.gc`, `.cal-*`) une fois l'agenda migré ; ce qui concerne `design/grid.css` et `static/` CSS hérités est nettoyé en T084.
- **EXPLAIN** de la requête sur `appointment` (plage × praticiens × cabinet) : vérifier l'index utilisé, joindre le plan à la PR et à Suivi.
- **Tests** : handler JSON (isolation par cabinet : un autre cabinet n'est pas visible ; praticien non autorisé ; bornes : plage > 42 jours, `end <= start`, dates invalides ; annulés ; buffers), semaine de **changement d'heure** (dernier dimanche de mars et d'octobre, fuseau `Europe/Paris`), non-régression `TestCalendarWeekWith150AppointmentsIsFast` **adapté à l'endpoint** (même seuil, 150 RDV).

Règles communes : `ui.css` est **généré** : `make css` après toute modif de template ; en cas de conflit au merge, **relancer `make css`** plutôt que résoudre à la main. Un seul agent à la fois sur les templates de layout. Pas de classe construite dynamiquement. CSP inchangée (pas de style ni script inline ; EventCalendar fonctionne sous CSP stricte : le vérifier).

## Critères d'acceptation
- [ ] Semaine, jour et vue par praticien fonctionnent ; buffers et indisponibilités visibles ; heures correctes sur une semaine de changement d'heure.
- [ ] Clic sur un RDV → dialogue ; chaque action existante fonctionne et rafraîchit l'agenda.
- [ ] JS désactivé : liste du jour utilisable, actions via la page du RDV.
- [ ] Endpoint : plage > 42 jours refusée, isolation par cabinet testée, aucune donnée de contact dans le JSON.
- [ ] EXPLAIN joint, index utilisé.
- [ ] Vérifié à 360 px (vue jour) et 1280 px (4 praticiens, semaine), clair et sombre, au clavier ; aucune erreur de console ni violation CSP.
- [ ] `make css` sans diff ; `make check` vert, aucun `style=` ajouté.

## Vérification
```bash
make css && git diff --exit-code internal/web/static/ui.css
make check
```

## Hors scope
Glisser-déposer, couleurs par praticien/motif (T057, à faire après), vue « Ma journée » (T072), formulaires de réglages (T082), retrait de l'ancien CSS (T084).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
