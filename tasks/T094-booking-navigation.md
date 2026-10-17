# T094 — Navigation de la prise de rendez-vous

- **Phase** : 5
- **Dépend de** : T081 (review)
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
- [ ] Instance à un seul cabinet : `/` → 303 vers `/p/{slug}` ; deux cabinets : page actuelle (tests handler).
- [ ] Saut de semaine : un samedi soir (horloge injectée), semaine courante vide → 303 vers la semaine du premier créneau ; `week` explicite → pas de saut ; aucun créneau dans l'horizon → pas de redirection en boucle (tests handler + service).
- [ ] Grille vide : lien « Voir la semaine suivante » présent ; navigation en bas ; un seul `<details open>`.
- [ ] « Premier créneau » redirige vers `/book?type=…&week=…` du praticien retenu (test handler).
- [ ] Tests DST uniquement si le calcul de semaine change.
- [ ] `make check` vert (dont `css-check` si un template change).

## Vérification
```bash
make sqlc && make css && make check
```

## Hors scope
- Changer le moteur de créneaux ou l'horizon de réservation.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** :
- **Fait** :
- **Reste à faire** :
- **Notes de reprise** :
- **Écarts avec la spécification** :
