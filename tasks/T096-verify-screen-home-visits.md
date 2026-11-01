# T096 — Écran de vérification et visites à domicile

- **Phase** : 5
- **Dépend de** : T094
- **Taille estimée** : S (< 200 lignes)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le patient peut corriger ses coordonnées depuis l'écran de vérification sans tout ressaisir, et connaît les zones desservies pour une visite à domicile.

Origine : audit UX des parcours patients du 2026-10-17.

## Contexte à lire
- `internal/web/templates/pages/public_verify.html`, `internal/web/templates/partials/booking_hidden.html`, `internal/web/public.go` (`renderDetails`), `public_waitlist.go` (`public_waitlist_verify`)
- `internal/web/templates/pages/public_details.html` (~l. 34, aide du champ adresse)
- `internal/service/practice.go` et `internal/web/admin_config.go` (~l. 209) : lecture de `practice.home_visit_zones`

## Spécification
1. **Modifier mes coordonnées** — dans `public_verify.html`, un `<form method="post">` qui reposte les champs cachés (`partials/booking_hidden.html`) vers un handler rendant `renderDetails` pré-rempli (200, sans erreur). Même chose sur l'écran OTP de la liste d'attente. Sans JS, avec CSRF.
2. **Zones de visite** — afficher les codes postaux / zones desservis (`practice.home_visit_zones`) dans l'aide du champ adresse de `public_details.html` quand la liste est non vide.

## Critères d'acceptation
- [x] Depuis l'écran de vérification, « Modifier mes coordonnées » ramène au formulaire pré-rempli, statut 200, aucun message d'erreur, aucun code OTP renvoyé (test handler).
- [x] Idem pour l'écran OTP de la liste d'attente.
- [x] Le jeton CSRF est requis sur ce POST (test handler).
- [x] Motif à domicile et zones non vides : zones affichées dans l'aide ; liste vide : rien (test handler).
- [x] `make check` vert (dont `css-check` si un template change).

## Vérification
```bash
make css && make check
```

## Hors scope
- Changer la logique OTP ou la validation des zones.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-11-01 (sonnet)
- **Fait** : routes `POST .../book/edit` et `POST .../waitlist/edit` (seau `search`) qui re-rendent le formulaire pré-rempli en 200 (créneau/motif revalidés par `bookingTarget`/`target`, ni OTP ni écriture) ; bouton « Modifier mes coordonnées » dans `public_verify.html` et `public_waitlist_verify.html` ; zones de visite (`Target.Practice.HomeVisitZones`, motif à domicile seulement) dans l'aide de l'adresse. Tests : `public_verify_edit_test.go`.
- **Reste à faire** : passage navigateur.
- **Notes de reprise** : liste de zones vide = motif à domicile non réservable en ligne (404), donc l'aide « vide » n'est pas atteignable ; le test vérifie qu'aucune aide n'apparaît. `TestPublicWaitlistJoinBlockedByNoShows` flake rarement (« 403 » présent dans un UUID de la page).
- **Écarts avec la spécification** : aucun (pas de CSS à régénérer, `ui.css` inchangé).
