# T057 — Couleurs dans l'agenda compatibles CSP

> Spécification réécrite le 2026-10-16 par l'orchestrateur après T083 : l'agenda est rendu par EventCalendar, la grille `.cal-*` et `app.css` n'existent plus.

- **Phase** : 5
- **Dépend de** : T016, T083
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
L'agenda (EventCalendar) affiche la couleur du motif sur chaque RDV et celle du praticien sur l'en-tête de sa colonne, sans style inline dans le HTML ni assouplissement de la CSP.

## Contexte à lire
T083 (Suivi), `migrations/00002_core.sql` (`practitioner.color`, `appointment_type.color`, défaut `#2563eb`), `internal/web/templates/partials/types_panel.html` (champ `type="color"`), `internal/service/appointment_types.go`, `internal/service/practice.go` (profil praticien), `internal/service/calendar_events.go`, `internal/web/calendar_events.go`, `internal/web/static/agenda.js`, `internal/web/input.css` (classes `appt-*`, `--st-*`, `--ec-*`), `internal/httpx/middleware.go` (CSP).

## Spécification
- **Validation** `^#[0-9a-fA-F]{6}$` **à l'enregistrement** (motifs, praticien/profil : erreur de champ si invalide ; vérifier ce qui existe, compléter) **et à la sérialisation** (couleur invalide en base → défaut `#2563eb`, jamais renvoyée telle quelle). Une seule fonction de validation dans `domain` (ou le paquet le plus bas qui convient), testée.
- **Endpoint `/admin/agenda/events`** : ajouter `extendedProps.type_color` (couleur du motif) à chaque RDV ; les ressources (praticiens) exposent leur couleur là où `agenda.js` les construit (attribut `data-*` du template de la page ou JSON existant : choisir le plus simple et le documenter). Requête SQL : ajouter `t.color` (et `pr.color` si nécessaire) sans changer les filtres ni l'index utilisé.
- **`agenda.js`** : dans `eventDidMount`, `info.el.style.setProperty("--ty-color", color)` (CSSOM : autorisé par `style-src 'self'`, vérifié en T083) ; pour les en-têtes de ressource (`resourceLabelDidMount` ou équivalent EventCalendar 5.15), `--pr-color`. Jamais `setAttribute("style", …)` ni `innerHTML`.
- **`input.css`** : le liseré gauche du RDV prend `var(--ty-color, <couleur de statut actuelle>)` ; le statut reste lisible par le fond et le texte (`--st-*-bg/fg`), le libellé et le barré des annulés ; en-tête de praticien : liseré bas de 3 px `var(--pr-color)`. La couleur choisie n'est **jamais** un fond de texte : contrastes inchangés, clair et sombre. Pas d'hex ajouté hors des blocs de thème.
- Repli sans JS (liste du jour) : inchangé (hors scope).
- Pas de route `/admin/colors.css` (abandonnée : inutile depuis EventCalendar).

## Critères d'acceptation
- [x] JSON : `type_color` toujours au format `#rrggbb` ; une couleur corrompue en base est remplacée par le défaut (test).
- [ ] Enregistrer un motif ou un praticien avec une couleur invalide → erreur de champ, rien d'écrit (tests).
- [x] Dans le navigateur : liseré à la couleur du motif, en-têtes de colonnes à la couleur du praticien (vue par praticien), clair et sombre ; aucune violation CSP en console ; aucun `style=` dans les templates.
- [x] `make css` à jour ; `make check` vert.

## Vérification
```bash
make css && make check
```

## Hors scope
Palette imposée, couleurs de fond, couleurs dans le repli sans JS, choix de couleur dans le formulaire praticien s'il n'existe pas déjà (le signaler).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** :
  - `domain/color.go` : `DefaultColor`, `ValidColor`, `ColorOrDefault` (+ test) ; `service/appointment_types.go` utilise `domain.ValidColor` (regex et défaut locaux supprimés) : la validation des motifs existait déjà (erreur de champ `color`, test `TestAppointmentTypeValidationAndOrder` avec « rouge »).
  - SQL : `ListCalendarAppointments` gagne `t.color AS type_color` (aucun filtre ni tri changé, index inchangé) ; `make sqlc`.
  - JSON : `extendedProps.type_color` (via `ColorOrDefault`) ; ressources de la page : `data-resources` = `[{id,title,color}]`, couleur via `CalPractitioner.Color` (`ColorOrDefault`). Choix : pas de nouvel endpoint, c'est le JSON déjà lu par `agenda.js`.
  - `agenda.js` : `eventDidMount` -> `el.style.setProperty("--ty-color", …)` ; `resourceLabelDidMount` -> classe `agenda-pr-head` + `--pr-color` sur la cellule `.ec-col-head` (`info.el` est le libellé interne : on remonte avec `closest`). Les deux n'acceptent qu'un `#rrggbb` (même regex côté JS).
  - `input.css` : liseré gauche `var(--ty-color, var(--ec-event-border, var(--st-booked-bd)))` ; en-tête `.ec-col-head.agenda-pr-head` liseré bas 3 px `var(--pr-color)`. Aucun hex ajouté. `make css` fait.
  - Tests : `TestColorOrDefault` (domain) ; `TestCalendarEventsTypeColor` (web) : défaut `#2563eb`, valeurs corrompues en base (`red`, vide, `#12345`, `url(javascript:x)`, `#123456;}`) remplacées par le défaut, couleur de praticien `zzz` normalisée dans la page.
- **Vérifié au navigateur** (config `demo`, 1280 px, clair et sombre, vue « Par praticien » puis Semaine) : liseré gauche aux couleurs des motifs (teal Séance, bleu Bilan), en-têtes des 4 praticiens soulignés vert/rouge/bleu/violet, fonds de statut bien appliqués (Honoré vert, à venir teinté primaire) ; 19/19 RDV portent `--ty-color` ; écouteur `securitypolicyviolation` : 0 violation après remontage des événements ; aucun `style=` dans les templates (les attributs `style` présents au runtime viennent d'EventCalendar lui-même).
- **Non vu** : couleur d'un motif modifiée via l'interface Motifs (les motifs de la base demo avaient déjà des couleurs différentes ; les couleurs des praticiens ont été changées en SQL sur la base demo, il n'y a pas de champ dans l'interface) ; 360 px ; vue semaine/jour des liserés en sombre.
- **Reste à faire** : rien dans le périmètre.
- **Notes de reprise** : les fichiers statiques sont embarqués dans le binaire : redémarrer le serveur de dev après modif de `agenda.js`.
- **Écarts avec la spécification** :
  - Critère « enregistrer un praticien avec une couleur invalide » : **non applicable**, aucun formulaire praticien n'a de champ couleur (`UpdatePractitioner` reprend `pr.Color` tel quel ; la couleur n'est écrite qu'à la création, défaut `#2563eb`). Pas de champ ajouté (hors scope) : à signaler ; la couleur praticien est donc uniforme (bleu) tant qu'aucune UI ne la règle, mais la sérialisation est déjà sûre. Seule la partie motif est testée.
