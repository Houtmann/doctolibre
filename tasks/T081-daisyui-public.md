# T081 — Parcours patient public en daisyUI

- **Phase** : 5
- **Dépend de** : T080
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard (+ captures 360 et 1280 px, clair et sombre)

## Objectif
Tout ce que voit un patient (réservation, vérification OTP, gestion du RDV par lien, liste d'attente publique, pages d'erreur) est rendu avec les composants daisyUI de T080.

## Contexte à lire
`poc/daisyui/booking.html` (référence visuelle), T080 (Suivi, `/dev/styleguide`, `docs/design-system.md`), `AGENTS.md` section Front, `internal/web/render_test.go`, tests de handlers publics.

## Spécification
Templates **de cette tâche** (et eux seuls) :
- Pages : `pages/public_practice.html`, `public_practitioner.html`, `public_book.html`, `public_details.html`, `public_verify.html`, `public_confirmed.html`, `public_cancel.html`, `public_reschedule.html`, `public_reschedule_confirm.html`, `public_privacy.html`, `public_waitlist.html`, `public_waitlist_verify.html`, `public_waitlist_offer.html`, `error.html` (rendu public ; la variante admin sera contrôlée en T084), `home.html`.
- Partials : `partials/booking_grid.html`, `booking_hidden.html`, `booking_summary.html`, `waitlist_hidden.html`.

Contenu : stepper des étapes (`steps`), cartes de motifs, grille de créneaux (boutons `btn`, jours repliables sur mobile), coordonnées, saisie OTP, récapitulatif. Aucune logique ni donnée nouvelle ; les `id`/`hx-target` des partials htmx sont conservés ; chaque formulaire fonctionne sans JS (POST + redirect) ; honeypot conservé (masqué par classe, pas de style inline).

Règles communes : `ui.css` est **généré** : après modification de templates, lancer `make css` ; en cas de conflit au merge sur `ui.css`, **relancer `make css`** plutôt que résoudre à la main. Un seul agent à la fois sur les templates de layout (non modifiés ici). Pas de classe construite dynamiquement. Les tests `httptest` qui cherchent d'anciennes classes `dl-*` sont adaptés (fragments clés, pas le HTML entier).

## Critères d'acceptation
- [ ] `grep -rn 'dl-' ` sur les templates listés ci-dessus : vide.
- [ ] Parcours complet (cabinet → praticien → créneau → coordonnées → OTP → confirmation, annulation, déplacement, liste d'attente) vérifié dans le navigateur à 360 et 1280 px, clair et sombre, au clavier.
- [ ] Formulaires utilisables sans JS (désactiver JS pour vérifier).
- [ ] Aucun débordement horizontal à 360 px.
- [ ] `make css` ne produit aucun diff après commit ; `make check` vert, aucun `style=` ajouté.

## Vérification
```bash
make css && git diff --exit-code internal/web/static/ui.css
make check
```

## Hors scope
Layouts et partials transverses (T080), back-office (T082), agenda (T083), retrait de l'ancien CSS (T084), nouvelles fonctionnalités (consignes par motif : T070).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
