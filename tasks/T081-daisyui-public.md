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
- [x] `grep -rn 'dl-' ` sur les templates listés ci-dessus : vide.
- [ ] Parcours complet (cabinet → praticien → créneau → coordonnées → OTP → confirmation, annulation, déplacement, liste d'attente) vérifié dans le navigateur à 360 et 1280 px, clair et sombre, au clavier.
- [ ] Formulaires utilisables sans JS (désactiver JS pour vérifier).
- [ ] Aucun débordement horizontal à 360 px.
- [x] `make css` ne produit aucun diff après commit ; `make check` vert, aucun `style=` ajouté.

## Vérification
```bash
make css && git diff --exit-code internal/web/static/ui.css
make check
```

## Hors scope
Layouts et partials transverses (T080), back-office (T082), agenda (T083), retrait de l'ancien CSS (T084), nouvelles fonctionnalités (consignes par motif : T070).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : les 14 pages et 4 partials listés migrés vers daisyUI (steps, cartes de motifs/praticiens en `card card-border`, jours de la grille en `<details class="collapse">` ouverts par défaut, créneaux en `btn btn-outline`, champs `input`/`checkbox`/`radio`, alertes, badges). `id`, `name`, `hx-*`, `action`, champs cachés, CSRF conservés ; honeypot masqué par `sr-only`. `make css` relancé, `make check` vert. `render_test.go` adapté (`<h1>` a maintenant des classes). `grep dl-` sur les templates listés : vide.
- **Reste à faire** (maj) : parcours réservation/annulation/déplacement vérifié par l'orchestrateur (base demo, clair et sombre) ; corrigé ensuite : nav de semaine en `flex-wrap` (libellé pleine largeur au-dessus des boutons sur mobile, `sm:` inchangé) et cases de `public_details` alignées (`checkbox-sm` + `text-sm`). Reste : liste d'attente publique, test sans JS, `scrollWidth` 360 sur `?week=2026-43` (nécessite redémarrage du serveur demo). Ancien texte : vérification navigateur du parcours complet (aucun cabinet en base `doctolibre_test`, pas de données de démo) ; relecture visuelle à 1280 px, clair/sombre, clavier et sans JS.
- **Notes de reprise** : seule la page 404 (error.html) a été vue au navigateur à 360 px (clair) : rendu correct, pas de débordement (scrollWidth 360). Créer un cabinet + praticien + motif + règle de disponibilité pour parcourir `/p/<slug>`.
- **Écarts avec la spécification** : stepper rendu en `<ul class="steps">` dans un `<nav>` (le « stepper » de la spec) ; les jours de créneaux utilisent `<details>` (repliables partout, pas seulement sur mobile, sans JS). Critères de parcours navigateur non cochés faute de données.
