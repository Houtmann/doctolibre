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
- [x] `make check` vert, aucun `style=` ajouté.
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

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : parcours public migré en v2 : `.dl-steps` (Motif / Créneau / Coordonnées / Vérification, `aria-current="step"`, sur practice, practitioner, book, details, verify) ; `.dl-choices`/`.dl-choice` (durée en `.dl-num`, badge `⌂ Domicile` via nouveau champ `TypeView.AtHome` renseigné dans `public.go` et `public_first.go`) ; `booking_grid` en `.dl-week-nav`/`.dl-slot-day`/`.dl-slots`/`.dl-slot`/`.dl-empty` (ids et `hx-*` inchangés) ; `booking_summary`, `public_confirmed`, `public_waitlist_offer` en `.dl-card` + `.dl-details` (badge Réservé/Annulé sur la confirmation) ; formulaires de `public_details`, `public_verify`, `public_waitlist`, `public_waitlist_verify` en `.dl-field/.dl-label/.dl-input/.dl-help`, `.dl-fieldset`, `.dl-check` ; alertes `dl-alert--error/--info` sur la page d'offre. `app.css` : retrait de `choice-list`, `week-nav`, `week-label`, `slot-day`, `slot-list`, `booking-summary`, `resend`, `home-badge`. `styleguide_test.go` : fragment `booking-summary` retiré (le partial est maintenant `dl-card`).
- **Composants ajoutés** (bloc `/* ── T066 ── */` en fin de `design/components.css` et `static/components.css`) : `.dl-field-row` (deux colonnes qui se replient), `.dl-resend`, `.dl-waitlist-cta`, marge du badge dans `.dl-choice`, libellé de `.dl-week-nav` pleine largeur au-dessus des boutons.
- **Reste à faire** : parcours clavier seul et captures avant/après (orchestrateur). Vérifié par moi dans le navigateur (démo, :8094, arrêté) : cabinet, praticien, créneaux, coordonnées à 360 px clair et sombre (vérification et confirmation non visitées : nécessitent un OTP).
- **Notes de reprise** : le critère grep `week-nav|detail-list` matche `dl-week-nav` (inévitable, c'est la classe v2) et `partials/appointment_detail.html` (`detail-list`, agenda : T067) ; `.detail-list` reste donc dans `app.css`. Le bloc T066 de `static/components.css` vient après le bloc `.dl-sg-*` ; conflit probable avec T065/T067 en fin de fichier (concaténer les blocs). `patient_fields.html` non touché (le formulaire public n'en dépend pas). Les titres h2 des cartes sont encore stylés par Pico (base v2 en T068). À 360 px, les libellés d'étapes passent à la ligne sous leur numéro.
- **Écarts avec la spécification** : étapes à 4 entrées (Vérification ajoutée à la carte à 3) ; pas de `.is-holiday` sur `.dl-slot-day` (la donnée n'est pas dans `GridDay`, et les jours sans créneau ne sont pas listés) ; pages de déplacement (`reschedule`) sans `.dl-steps` ; pas de `.dl-steps` sur la liste d'attente.
- **Contrôle orchestrateur (2026-10-16)** : `make check` ✅ après fusion ; `.dl-field-row` en double avec T065 dédoublonné à la fusion (version T065 gardée). Navigateur : praticien et créneaux à 375 px sombre (étapes, choix, état vide) ; OTP et confirmation non revus (code OTP requis). Grep du critère : `week-nav` matche `dl-week-nav`, `detail-list` a disparu avec T067.
- **Clôture (2026-10-16)** : `done` — remplacée par T080–T084 (migration daisyUI, voir `docs/decisions.md`). Le travail est mergé mais son CSS `dl-*` est supprimé en T084.
