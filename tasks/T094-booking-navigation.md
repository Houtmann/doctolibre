# T094 — Navigation de la prise de rendez-vous

- **Phase** : 5
- **Dépend de** : T081
- **Taille estimée** : M (< 500)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
La prise de RDV guide le patient : accueil vers le cabinet unique, saut vers la première semaine disponible, grille avec appel à l'action clair, « premier créneau disponible » qui laisse choisir l'horaire.

Origine : audit UX des parcours patients du 2026-10-17.

## Contexte à lire
- `internal/web/routes.go` (~l. 85), `internal/web/templates/pages/home.html`, `internal/web/templates/layout/public.html` (l. 3, logo)
- `internal/web/public.go` (`/book` sans `week`, ~l. 314-318), `internal/service/booking_first.go` (`Booking.FirstAvailable` ~l. 59 : fenêtres de 14 j, arrêt anticipé)
- `internal/web/templates/partials/booking_grid.html`, `internal/web/public_first.go`
- `internal/store/queries/practice.sql` (nouvelle requête), `docs/design-system.md`

## Spécification
1. **Accueil** — `/` : si l'instance a exactement un cabinet, 303 vers `/p/{slug}` ; sinon page actuelle. Nouvelle requête sqlc bornée (`LIMIT 2`) ; lancer `make sqlc`. Le logo de `layout/public.html` pointe vers `/p/{slug}` quand la page connaît le cabinet, sinon `/`.
2. **`/book` sans `week`** — si la semaine courante n'a aucun créneau, 303 vers la semaine du premier créneau du praticien pour ce motif, dans l'horizon de réservation. Ajouter une variante par praticien de `Booking.FirstAvailable` plutôt que dupliquer. Avec `week` explicite : pas de saut.
3. **Grille `booking_grid.html`** — message vide : vrai bouton/lien « Voir la semaine suivante » (`btn btn-primary`), plus visible que « Aucun créneau ne me convient » ; navigation de semaine dupliquée en bas ; seul le premier jour avec créneaux est `open`, les autres repliés. Aucune classe dynamique, fonctionne sans JS.
4. **« Premier créneau disponible »** (`public_first.go`) — rediriger vers la grille du praticien retenu à la semaine du créneau (`/book?type=…&week=…`) au lieu de `/book/details`, pour que le patient choisisse l'horaire.

## Critères d'acceptation
- [x] Instance à un seul cabinet : `/` → 303 vers `/p/{slug}` ; deux cabinets : page actuelle (tests handler).
- [x] Saut de semaine : un samedi soir (horloge injectée), semaine courante vide → 303 vers la semaine du premier créneau ; `week` explicite → pas de saut ; aucun créneau dans l'horizon → pas de redirection en boucle (tests handler + service).
- [x] Grille vide : lien « Voir la semaine suivante » présent ; navigation en bas ; un seul `<details open>`.
- [x] « Premier créneau » redirige vers `/book?type=…&week=…` du praticien retenu (test handler).
- [x] Tests DST uniquement si le calcul de semaine change.
- [x] `make check` vert (dont `css-check` si un template change).

## Vérification
```bash
make sqlc && make css && make check
```

## Hors scope
- Changer le moteur de créneaux ou l'horizon de réservation.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-11-01 (sonnet).
- **Fait** : requête `ListPracticeSlugsLimit2` + `Booking.SoleSlug` ; `/` → 303 vers `/p/{slug}` si un seul cabinet ; logo des pages publiques via `View.HomeURL` ; `Booking.FirstAvailable` factorisé (`firstFor`) + variante `FirstAvailableFor` (par praticien) ; saut de semaine dans `grid` (cible bornée [minMon, maxMon], pas de saut si `week` présent dans l'URL, aucun créneau dans l'horizon = semaine courante) ; grille : bouton « Voir la semaine suivante », navigation répétée en bas (`grid_nav`), seul le premier jour `open`, lien liste d'attente adouci ; « Premier créneau » redirige vers `/book?type&week` du praticien retenu (`FirstSlot.Practice` ajouté). Tests : `TestPublicBookingJumpsToFirstWeek`, `TestHomeRedirectsToSolePractice`, `TestFirstAvailablePractitioner` mis à jour ; trois tests existants adaptés (URL de grille avec `week` ou saut suivi).
- **Reste à faire** : passage navigateur (non vu à l'écran).
- **Notes de reprise** : `make check` vert, 0 SKIP côté web. `ui.css` inchangé (classes déjà présentes).
- **Écarts avec la spécification** : `week=` vide (présent mais vide) ne saute pas (seule l'absence du paramètre déclenche le saut). La grille de déplacement T095 (`public_manage.go`) n'est pas touchée, mais partage le partial `booking_grid` (nav en bas, premier jour ouvert). Pas de test DST ajouté (calcul de semaine inchangé).
