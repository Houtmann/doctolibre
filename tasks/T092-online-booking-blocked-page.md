# T092 — Page patient « réservation en ligne indisponible »

- **Phase** : 5
- **Dépend de** : T088
- **Taille estimée** : S (< 200 lignes)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le refus de réservation en ligne pour absences répétées (T054) affiche au patient une page dédiée, sobre et utile (appeler le cabinet, revenir à sa page), au lieu de la page d'erreur générique « 403 / Accès refusé ».

## Contexte à lire
- `internal/web/public.go` : cas `domain.ErrOnlineBookingBlocked` (~l. 599) et `onlineBookingBlockedMsg` (~l. 613).
- `internal/web/public_waitlist.go` : cas `ErrOnlineBookingBlocked` de l'inscription (~l. 241) et de `claim` (~l. 296).
- `internal/web/templates/pages/error.html` et le rendu `rd.Error` (pour voir ce qu'on remplace) ; une page publique existante pour le layout, ex. `pages/public_confirmed.html`.
- `docs/design-system.md` (partials, alertes daisyUI) ; décisions du 2026-10-17 (T054, T088) dans `docs/decisions.md`.

## Spécification
- Nouveau template `internal/web/templates/pages/public_booking_blocked.html`, layout public (en-tête du cabinet comme les autres pages publiques), daisyUI :
  - titre neutre « Réservation en ligne indisponible » ; **aucun code HTTP** ni « Accès refusé » affiché ;
  - texte neutre qui ne mentionne pas les absences (décision T054) : la réservation en ligne n'est pas possible, merci d'appeler le cabinet ;
  - si le cabinet a un téléphone : bouton/lien `tel:` (numéro normalisé : chiffres et `+` uniquement dans l'attribut `href`, numéro affiché tel quel) ; sinon « Merci de contacter le cabinet. » ;
  - lien de retour vers la page du praticien (ou du cabinet si le praticien n'est pas connu), pas vers `/`.
- Une seule fonction de rendu partagée (ex. `renderBookingBlocked(w, r, practice, backURL)` dans `public.go`) qui répond **403**, utilisée par les trois cas : réservation (`Book`), inscription liste d'attente, `/w/{token}` (`claim`). Pour `claim`, réutiliser `h.svc.Offer` comme aujourd'hui pour récupérer cabinet/praticien ; si `Offer` échoue, page sans téléphone avec retour vers la page du cabinet si connue, sinon sans lien de retour.
- Requête htmx (`HX-Request`) : même contenu, même statut ; suivre la convention existante du handler (si les pages publiques renvoient un fragment en htmx, faire de même ; sinon page complète avec `HX-Retarget`/`HX-Reswap` comme ailleurs — ne pas inventer de nouveau mécanisme).
- Supprimer `onlineBookingBlockedMsg` si plus utilisée.
- `make css` après ajout du template (classes daisyUI écrites en entier).

## Critères d'acceptation
- [x] Réservation refusée (patient bloqué) : statut 403, page « Réservation en ligne indisponible », pas de « 403 » ni « Accès refusé » dans le corps, lien `href="tel:…"` présent quand le cabinet a un téléphone, lien de retour vers la page du praticien.
- [x] Même chose pour l'inscription en liste d'attente et pour `/w/{token}` (Claim).
- [x] Cabinet sans téléphone : pas de lien `tel:`, texte « contacter le cabinet ».
- [x] Tests handler (`httptest` sur `web.Routes`) pour les trois parcours (étendre les tests T054/T088 existants).
- [x] `make check` vert (dont `css-check`).

## Vérification
```bash
make css && make check
```

## Hors scope
- Changer la règle de blocage, le seuil ou le moment du contrôle (T054).
- Retoucher la page d'erreur générique `error.html`.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-17 (sonnet)
- **Fait** : page `public_booking_blocked.html` ; `renderBookingBlocked` (public.go, 403, no-store) utilisée par Book, inscription liste d'attente et Claim ; `onlineBookingBlockedMsg` supprimée ; tests (`assertBookingBlocked` dans no_show_guard_test.go, 3 parcours + cabinet sans téléphone via Claim) ; cas ajouté à render_test.go.
- **Reste à faire** : relecture humaine. Navigateur (orchestrateur, `doctolibre_demo`) : inscription en liste d'attente de « Test Navigateur » → 403, page conforme, `tel:0102030405`, retour `/p/cabinet-demo/claire-martin`, 1024 px clair et 360 px sombre ; réservation et Claim non vus au navigateur (même rendu, tests).
- **Notes de reprise** : `ui.css` inchangé (classes déjà présentes).
- **Écarts avec la spécification** : si `Offer` échoue dans Claim, aucun lien de retour (cabinet inconnu). En htmx, page complète comme `rd.Error` avant (pas de mécanisme nouveau).