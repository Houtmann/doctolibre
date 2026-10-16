# T066 — Design v2 : parcours patient

- **Phase** : 5
- **Dépend de** : T064
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard (+ captures avant/après clair/sombre, 360 et 1280 px)

## Objectif
Le parcours de réservation public (cabinet → praticien → motif → créneau → coordonnées → vérification → confirmation) et les pages de gestion du RDV par le patient (annulation, déplacement, liste d'attente) suivent la carte `booking-flow`.

## Contexte à lire
`design/README.md`, `design/MIGRATION.md`, la ou les cartes `design/previews/` citées ci-dessous, `/dev/styleguide/v2` (T063), pages `home`, `public_*.html`, partials `booking_grid`, `booking_summary`, `booking_hidden`, `waitlist_hidden`, `patient_fields` (seulement l'enveloppe si T065 n'est pas mergée : coordonner via Suivi). Cartes : `booking-flow`, `card-summary`, `badges`.

## Spécification
- `.dl-steps` (étape courante `aria-current="step"`) en tête de chaque étape du parcours.
- Choix de praticien/motif : `.dl-choices` + `.dl-choice` (cible ≥ 44 px, durée en `.dl-num`, badge `⌂ Domicile` à la place de `.home-badge`).
- Créneaux : `.dl-week-nav`, `.dl-slot-day`, `.dl-slots` + `.dl-slot` ; état vide `.dl-empty`.
- Récapitulatif et confirmation : `.dl-card` + `.dl-details` (remplace `.booking-summary`, `.detail-list`).
- Règles communes : remplacer les classes v1 par les classes v2 selon `design/MIGRATION.md` ; les tests `httptest` qui cherchent d'anciennes classes sont mis à jour (fragments v2) ; aucun `style=` ni script inline ; les formulaires marchent toujours sans JS ; les variantes htmx des partials gardent les mêmes `id`/`hx-target`. Si une classe v1 n'est plus utilisée nulle part après la tâche, la retirer d'`app.css` ; sinon la laisser (T068 nettoie).
- Composant manquant ou inadapté : l'ajouter dans `internal/web/static/components.css` **et** `design/components.css` (jetons sémantiques uniquement), et le signaler dans Suivi.

## Critères d'acceptation
- [ ] `grep -rnE 'choice-list|slot-list|week-nav|booking-summary|detail-list|home-badge' internal/web/templates` vide.
- [ ] Parcours complet réalisé au clavier seul dans le navigateur (orchestrateur).
- [ ] `make check` vert, aucun `style=` ajouté.
- [ ] Vérifié dans le navigateur à 360 et 1280 px, clair et sombre, et au clavier (focus visible).

## Vérification
```bash
make check
grep -rnE 'choice-list|slot-list|week-nav|booking-summary|detail-list|home-badge' internal/web/templates
```

## Hors scope
Formulaires admin (T065), agenda (T067), retrait de Pico (T068), toute modification de logique de réservation.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
