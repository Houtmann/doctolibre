# T057 — Couleurs dans l'agenda compatibles CSP

> UI à faire en daisyUI ; à lancer après T080 (décision du 2026-10-16). Agenda : EventCalendar (T083) — les couleurs passent par les propriétés d'événement/classes de l'agenda, plus par `.cal-*` ; reprendre la spécification après T083.

- **Phase** : 5
- **Dépend de** : T016
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
L'agenda affiche la couleur du motif et celle du praticien malgré la CSP interdisant les styles inline.

## Contexte à lire
`migrations/00002_core.sql` (`practitioner.color` l. ~24, `appointment_type.color` l. ~65, défaut `#2563eb`), `internal/web/templates/partials/types_panel.html` (champ `type="color"`), `internal/service/appointment_types.go`, `internal/service/practice.go`, `internal/web/calendar.go`, `internal/web/templates/pages/admin_calendar.html`, `internal/web/static/app.css` (classes `.cal-*`), `internal/web/routes.go` (middlewares, CSP).

## Spécification
- Route `GET /admin/colors.css` (authentifiée, même origine, `Content-Type: text/css`) : pour le cabinet de l'utilisateur, `.pr-<short-id>{--pr-color:#hex}` et `.ty-<short-id>{--ty-color:#hex}` (`short-id` = 8 premiers caractères hex de l'UUID sans tirets, ou l'UUID complet si collision). `ETag` + `Cache-Control: private, max-age=60`. Lien `<link rel="stylesheet">` dans le layout admin.
- Couleurs validées `^#[0-9a-fA-F]{6}$` **à l'enregistrement ET à la génération** (couleur invalide → défaut, jamais de CSS injecté). Vérifier la validation actuelle à l'enregistrement des motifs et du profil, la compléter si absente.
- `app.css` : liseré gauche du bloc RDV = `var(--ty-color)` (classe `ty-…` posée par le template), en-tête de colonne du praticien = `var(--pr-color)`. Le statut (annulé, absent…) garde son traitement actuel. La couleur n'est **jamais** le fond du texte : contraste AA inchangé.
- Pas de style inline, pas de ressource externe.

## Critères d'acceptation
- [ ] `/admin/colors.css` ne contient que des déclarations valides ; une couleur corrompue en base est remplacée par le défaut.
- [ ] Les blocs RDV et en-têtes de colonnes de l'agenda portent les classes et s'affichent colorés (vérifier dans le navigateur).
- [ ] Anonyme → redirection login ; autre cabinet → ses couleurs seulement.
- [ ] Tests : génération CSS (golden ou fragments), validation, ETag 304.

## Vérification
```bash
make check
```

## Hors scope
Palette imposée, couleurs de fond des textes, thème sombre.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
