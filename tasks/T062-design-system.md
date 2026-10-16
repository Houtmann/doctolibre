# T062 — Design system léger : tokens, catalogue, styleguide

- **Phase** : 5
- **Dépend de** : T004, T016
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Avoir un socle visuel unique et documenté : les nouvelles pages de la phase 5 réutilisent les mêmes tokens et partials au lieu de réinventer couleurs, messages et boutons.

## Contexte à lire
`internal/web/static/app.css` (bloc `:root`, classes `.flash-*`, `.form-errors`, `.cal-*`, ~14 couleurs hex en dur dont des doublons l. ~123–142), `internal/web/templates/partials/` (15 partials), `internal/web/templates/layout/`, `internal/web/routes.go` (montage des routes, CSP), `internal/config/config.go` (`EnvDev`), `AGENTS.md` (section Front), `tasks/T057-calendar-colors.md` (couleurs praticien/motif, à ne pas implémenter ici).

## Spécification
- **Tokens** : en tête de `app.css`, un bloc `:root` unique et commenté par famille. Préfixe `--app-`, en s'appuyant sur les variables Pico (`--pico-*`) quand elles suffisent.
  - Couleurs sémantiques : `success`, `error`, `info`, `warning`, chacune en `-bg` / `-fg`.
  - États de RDV : `--app-st-booked-*`, `--app-st-done-*`, `--app-st-no_show-*`, `--app-st-cancelled-*` (bg, fg, border).
  - Agenda : bande hachurée, ligne « maintenant ».
  - Espacements (`--app-space-1..4`) et rayon (`--app-radius-sm`).
- **Plus aucune couleur hex en dur hors du bloc `:root`.** Remplacer les occurrences existantes (`.cal-appt`, `.cal-list a`, `.cal-band`, `.cal-now`…) par les tokens, **sans changement visuel en mode clair** : les valeurs actuelles deviennent les valeurs des tokens.
- **Mode sombre** : Pico bascule déjà en sombre (`prefers-color-scheme`), mais nos couleurs claires en dur restent claires. Redéfinir les tokens `--app-*` sous `@media (prefers-color-scheme: dark)`, avec la même garde que Pico (`:root:not([data-theme="light"])`). Contraste AA (≥ 4,5:1 texte/fond) pour chaque paire bg/fg, dans les deux thèmes ; consigner les ratios calculés dans `docs/design-system.md`.
- **Catalogue** : nouveau `docs/design-system.md`, court (une page). Il contient :
  - la liste des tokens et leur usage ;
  - pour chaque partial réutilisable (`flash`, `form_errors`, `patient_fields`, `status_button`…) : données attendues, variante htmx, exemple d'appel `{{template …}}` ;
  - les règles de composition : un message = `flash`, une erreur de formulaire = `form_errors`, pas de nouvelle classe de couleur sans token.
  - Lien depuis `docs/README.md` et une ligne dans la section Front d'`AGENTS.md` (« réutiliser les tokens et partials de `docs/design-system.md` »). Exception au périmètre exécutant : ces deux ajouts sont prévus par la tâche.
- **Page styleguide** : `GET /dev/styleguide`, montée **uniquement si `Env == EnvDev`** (404 en prod, testé). Template `pages/dev_styleguide.html`, layout public.
  - Elle affiche : nuancier des tokens (une classe CSS par pastille, pas de style inline), boutons Pico (primaire, secondaire, contour, désactivé), formulaire type avec `form_errors`, les 4 `flash`, un extrait d'agenda avec un RDV par état, et chaque partial réutilisable avec des données factices construites en Go dans le handler (pas de DB).
  - Elle sert de page de contrôle pour le rendu 360 px / 1280 px.
- Pas de nouvelle dépendance, pas de style ni de script inline, pas de ressource externe.

## Critères d'acceptation
- [x] `grep -nE '#[0-9a-fA-F]{3,6}\b' internal/web/static/app.css` ne remonte que des lignes du ou des blocs `:root`.
- [x] Rendu clair inchangé (comparer captures avant/après de l'agenda semaine et d'une page avec flash) ; rendu sombre lisible.
- [x] Ratios AA consignés pour chaque paire bg/fg, clair et sombre.
- [x] `/dev/styleguide` : 200 en dev, 404 en prod (test `httptest`) ; contient les fragments clés (un `flash-success`, un `st-no_show`, un `form-errors`).
- [x] `docs/design-system.md` créé, lié depuis `docs/README.md` et `AGENTS.md`.
- [x] Vérifié dans le navigateur à 360 px et 1280 px, clair et sombre.

## Vérification
```bash
make check
grep -nE '#[0-9a-fA-F]{3,6}\b' internal/web/static/app.css
```

## Hors scope
Theming par cabinet, couleurs praticien/motif (T057), refonte visuelle, nouveaux composants, remplacement de Pico, outillage JS (Storybook…).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : jetons `--app-*` (sémantiques, états de RDV, agenda, espacements, rayon) dans `app.css`, thème sombre (media + `[data-theme="dark"]`), plus aucun hex hors `:root` ; `docs/design-system.md` (jetons, ratios, catalogue des partials) ; liens dans `docs/README.md` et `AGENTS.md` ; `GET /dev/styleguide` (dev seulement, `internal/web/styleguide.go`, `pages/dev_styleguide.html`, tests `styleguide_test.go`, cas ajouté à `render_test.go`) ; kind de flash `warning` accepté (`render.go`) + classe `.flash-warning`.
- **Reste à faire** : vérification navigateur 360/1280 px clair/sombre et comparaison de captures avant/après (orchestrateur). Lancer : `DOCTOLIBRE_ENV=dev DOCTOLIBRE_SECRET_KEY=x… go run ./cmd/doctolibre serve` (cf. `.env.example`), puis `/dev/styleguide`.
- **Hex d'origine de `app.css` (ligne → valeur)**, tous conservés tels quels en clair : l.4 `#e6f4ea` success-bg, l.5 `#0b4a1d` success-fg, l.6 `#fdecea` error-bg, l.7 `#7a1010` error-fg, l.8 `#e8f0fe` info-bg, l.9 `#0b2f6b` info-fg ; l.123 `#ececec`/`#f5f5f5` bande (band-a/b) ; l.129 et l.140 `#dbe8ff` fond + `#0b2f6b` texte (st-booked, liseré = ex `--app-info-fg` = `#0b2f6b`) ; l.131 et l.141 `#d9f0df`/`#0b4a1d`/`#0b4a1d` (st-done) ; l.132 et l.142 `#ffe6cc`/`#6b3200`/`#6b3200` (st-no_show) ; l.135 `#c0171d` ligne « maintenant ». Nouveaux (sans effet sur l'existant) : warning `#fff4d6`/`#5c4400`, st-cancelled `#ececec`/`#3d3d3d`/`#6b6b6b` (aucune règle visible tant que le calendrier n'affiche pas les annulés).
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : touche `app.css` comme T057 et T061 → ne pas lancer en parallèle de ces deux tâches ; les faire après T062 pour qu'elles réutilisent les tokens.
- **Écarts avec la spécification** (et pourquoi) : classe `.flash-warning` et kind `warning` ajoutés (nécessaires pour « les 4 flash ») ; `render_test.go` complété d'un cas `dev_styleguide` (le test générique de toutes les pages en a besoin). Espacements `--app-space-*` utilisés seulement par `.flash`/`.form-errors` et le styleguide (pas de refactor global).
- **Contrôle orchestrateur (2026-10-16)** : correspondance hex → tokens relue ligne à ligne (valeurs claires identiques, liseré booked = même `#0b2f6b`) ; `/dev/styleguide` vérifié dans le navigateur à 1280 px clair et 360 px sombre (pas de débordement horizontal, aucun `style=`, fond booked calculé `rgb(219,232,255)` en clair, `rgb(20,39,77)` en sombre). Constatés en passant, antérieurs à T062 : puce carrée Pico et `<small>` à la ligne dans `patient_results` ; la CSP bloque les icônes SVG `data:` de Pico (case à cocher, champ date).
