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
- [x] Semaine, jour et vue par praticien fonctionnent ; buffers et indisponibilités visibles ; heures correctes sur une semaine de changement d'heure.
- [x] Clic sur un RDV → dialogue ; chaque action existante fonctionne et rafraîchit l'agenda.
- [x] JS désactivé : liste du jour utilisable, actions via la page du RDV.
- [x] Endpoint : plage > 42 jours refusée, isolation par cabinet testée, aucune donnée de contact dans le JSON.
- [x] EXPLAIN joint, index utilisé.
- [ ] Vérifié à 360 px (vue jour) et 1280 px (4 praticiens, semaine), clair et sombre, au clavier ; aucune erreur de console ni violation CSP.
- [x] `make css` sans diff ; `make check` vert, aucun `style=` ajouté.

## Vérification
```bash
make css && git diff --exit-code internal/web/static/ui.css
make check
```

## Hors scope
Glisser-déposer, couleurs par praticien/motif (T057, à faire après), vue « Ma journée » (T072), formulaires de réglages (T082), retrait de l'ancien CSS (T084).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** :
  - SQL : `ListCalendarAppointments` remplacée (plusieurs praticiens via `ANY(@practitioner_ids)`, `practice_id`, bornes de `during_blocked`) ; `service/calendar_events.go` : `AgendaRange` (RDV non annulés + indisponibilités jour par jour avec `AddDate`, fériés et absences) et `AgendaDay` (liste du jour, repli) ; suppression de `View`, `buildColumn`, `CalDay/CalBand/CalSlot/CalHour`, `wallMinutes`, `ceilDiv`.
  - `web/calendar_events.go` : `GET /admin/agenda/events` (JSON, erreurs JSON 400/403/404, `Cache-Control: no-store`). Dates : RFC 3339 ou heure murale sans décalage (lue dans le fuseau du cabinet) ; plage max 42 jours calendaires, `end > start`.
  - EventCalendar 5.15.0 vendored (`static/vendor/` : js, css, `VERSION` avec URL et sha256, `LICENSE-event-calendar` MIT récupérée sur le dépôt) ; `static/agenda.js` ; `input.css` (`@source agenda.js`, fonds par statut, indisponible, hachures pour les buffers) ; templates agenda, détail, formulaire, bouton de statut migrés en daisyUI ; `calendar_grid.html` supprimé.
  - Dialogue `<dialog class="modal">` rempli en htmx ; action réussie dans le dialogue = 204 + `HX-Trigger: {"agenda:changed":{"date":"..."}}` (pas de flash ni redirection) ; `agenda.js` ferme, va à la date et fait `refetchEvents()`. Hors dialogue (page plein écran) : POST classique + redirection, sans attributs `hx-*` sur les formulaires (`.Modal` = requête htmx).
  - Tests : service (`TestAgendaRangeDST` 25/10 et 29/03 avec formes de date sans décalage et RFC 3339, bornes/validation, annulés, isolation cabinet, confrère 403, liste du jour) ; web (page, JSON sans contact, validation 400, 404 autre cabinet, 403 confrère, semaine DST, fériés, `TestCalendarWeekWith150AppointmentsIsFast` sur l'endpoint, seuil 100 ms inchangé : ~1,3 ms).
- **EXPLAIN** (base de test dédiée, 610 740 RDV, 60 praticiens, 20 cabinets ; semaine de 150 RDV, 3 praticiens) : `Bitmap Index Scan on appointment_no_overlap` (cond. `practitioner_id = ANY(...) AND during_blocked && ...`), puis Bitmap Heap Scan filtré `practice_id` ; `Execution Time: 3.9 ms`. Plan résumé : Sort -> Nested Loop (appointment_type, patient : 20 lignes chacun) -> Bitmap Heap Scan on appointment <- Bitmap Index Scan on appointment_no_overlap.
- **Vérifié au navigateur** (volet du client, ~800 px de large ; serveur local sur base jetable, supprimée) : semaine et « par praticien » à 4 praticiens (clair), dimanche 25/10/2026 (heures 09:00 et 11:00 correctes après correction), indisponibilités et hachures de buffer, clic sur un créneau -> formulaire en dialogue -> création -> fermeture + rafraîchissement, clic RDV -> dialogue -> « Marquer honoré » -> rafraîchissement, vue jour à 360 px en sombre. Aucune violation CSP ni erreur de console avec la version finale (le journal de la console contenait des erreurs d'une première version, corrigée).
- **Reste à faire / non vu** : vue 1280 px réelle (le volet fait ~800 px), mode sombre à 1280, navigation au clavier de l'agenda (Tab, Entrée sur un RDV : `tabindex`, `role=button` et `keydown` posés mais non essayés), déplacer/annuler dans le dialogue (couverts seulement par les tests HTTP), JS désactivé dans un vrai navigateur (couvert par les tests : liste du jour, formulaires sans `hx-*`).
- **Notes de reprise** :
  - EventCalendar est utilisé avec `timeZone: "UTC"` sur l'heure murale du cabinet : le serveur envoie des dates avec décalage, `agenda.js` retire le décalage. Raison : en fuseau local du navigateur (et aussi avec `timeZone: "Europe/Paris"`), la grille est décalée d'une heure après 03:00 le dimanche du retour à l'heure d'hiver (vu : RDV de 09:00 affiché à 10:00). Conséquences : jamais utiliser les objets `Date` d'EventCalendar (ce sont des dates du navigateur), seulement `startStr`/`dateStr` ; `nowIndicator` désactivé (il serait en UTC) ; la mise en évidence d'« aujourd'hui » peut être fausse 1 à 2 h après minuit.
  - `eventSources: [{events: fn}]` (l'option `events` n'accepte que des tableaux).
  - Le CSS d'EventCalendar est chargé par un `<link>` dans le contenu de la page agenda (valide en HTML5 dans `<body>`), idem les scripts en `defer` : aucun layout touché ; n'est donc pas importé dans `input.css`, ce qui évite de l'alourdir (15 Ko) sur toutes les pages. Les surcharges `.ec.ec` d'`input.css` l'emportent par spécificité.
  - Semaine ou jour (hors « par praticien ») : seul le premier praticien sélectionné est affiché (comme l'ancienne vue semaine) ; le sélecteur à cases reste un formulaire GET classique, dont `agenda.js` tient les champs `view` et `date` à jour.
  - Les RDV annulés ne sont pas servis par l'endpoint (comme l'ancien agenda) ; les classes `appt-cancelled` / `--st-cancelled-*` restent prêtes.
  - Le serveur de dev perd les sessions à chaque redémarrage (se reconnecter).
- **Écarts avec la spécification** :
  - JSON : `resourceId` (un seul praticien par RDV, EventCalendar l'accepte au lieu de `resourceIds`) ; statut `cancelled` jamais renvoyé (voir ci-dessus).
  - Titre = « Nom Prénom » (ce que l'ancienne grille affichait), pas abrégé.
  - Ligne « maintenant » de la vue jour perdue (voir notes). Pas de lien « créneau » au clavier : un bouton « Nouveau rendez-vous » est présent à la place.
  - Les fonctions de `service` ne sont plus `View`/`ViewInput` : `AgendaRange`, `AgendaDay`, `Selection`.
  - `team_test.go` et `holidays_test.go` adaptés (hors liste de la tâche, ils testaient la grille supprimée).
