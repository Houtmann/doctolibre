# T084 — Pages restantes, retrait de Pico et de l'ancien CSS

- **Phase** : 5
- **Dépend de** : T081, T082, T083
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard (+ passage de contrôle complet, captures 360 et 1280 px, clair et sombre)

## Objectif
Plus aucune page ne dépend de Pico ni du CSS maison : `ui.css` généré est la seule feuille de style applicative. Les dossiers `design/` et `poc/` ne sont plus nécessaires. Absorbe T068.

## Contexte à lire
T080–T083 (Suivi), `internal/web/static/`, `internal/web/styleguide.go`, `internal/web/render_test.go`, `internal/web/templates/`, `AGENTS.md`, `docs/design-system.md`, `design/README.md`, `design/MIGRATION.md`, `poc/daisyui/`, `docs/decisions.md`.

## Spécification
Templates **de cette tâche** (et eux seuls) : `pages/login.html`, `password_forgot.html`, `password_reset.html`, `invitation.html`, `admin_dashboard.html`, `admin_status.html`, `error.html` (contrôle de la variante admin ; le rendu public est fait en T081), et le retrait de `pages/dev_styleguide_v2.html`.

- Migrer ces pages en daisyUI (mêmes règles que T081/T082 : formulaires sans JS, champs en erreur marqués, `id`/`hx-target` conservés).
- **Garde-fou avant suppression** : lister les éléments que Pico stylisait par élément (`button`, `a[role=button]`, `input`, `select`, `textarea`, `table`, `dialog`, `article`, `nav ul`, `details`, `progress`) encore présents sans classe daisyUI ; migrer chaque occurrence. Liste et traitement dans Suivi.
- **Supprimer** : `static/pico.min.css`, `tokens.css`, `components.css`, `base.css`, `grid.css` éventuel, `app.css` écrit à la main, leurs liens dans les layouts, les partials/classes `dl-*` inutilisés, les styles `.dl-sg-*`, la route et le test `/dev/styleguide/v2`. Corriger la CSP si elle ne devait ses assouplissements qu'à Pico (icônes `data:`).
- **`design/` et `poc/`** : supprimer (l'historique git les conserve) ou archiver hors du dépôt ; si une valeur de `design/` reste utile, elle est déjà dans les thèmes d'`input.css`. Mettre à jour `docs/README.md` et tout lien vers ces dossiers.
- Documentation : retirer de `docs/design-system.md`, `AGENTS.md`, `docs/progress/` (hors journal) les références à Pico et à `--app-*`/`--dl-*` ; ajouter une ligne à `docs/decisions.md` si un choix imprévu est fait.
- **Passage de contrôle complet** (consigné dans Suivi, page par page : liste de `render_test.go` + agenda) : 360 px et 1280 px, clair et sombre, contrastes AA, navigation au clavier (ordre, focus visible, dialogue : piège de focus et `Échap`), pas de débordement horizontal, aucune erreur de console ni violation CSP.
- Taille du binaire avant/après notée dans Suivi.

## Critères d'acceptation
- [x] `grep -rniE 'pico|--app-|--dl-|dl-' internal/ docs/design-system.md AGENTS.md PLAN.md` vide (hors `decisions.md`/journal).
- [x] `grep -rnE '<(button|select|textarea|table|dialog|article)\b' internal/web/templates | grep -v 'class='` vide ou chaque ligne justifiée dans Suivi.
- [x] Les fichiers CSS hérités et `design/`, `poc/` sont supprimés ; `internal/web/static/` ne contient plus que `ui.css` (généré), `app.js`, `agenda.js`, `htmx.min.js`, `vendor/`.
- [ ] Toutes les pages vérifiées (contrôle ci-dessus) ; défauts d'affichage repérés dans STATUS.md levés ou notés.
- [x] `make css` sans diff ; `make check` vert, aucun `style=`.

## Vérification
```bash
make css && git diff --exit-code internal/web/static/ui.css
make check
grep -rniE 'pico|--app-|--dl-' internal/ docs/design-system.md AGENTS.md PLAN.md
```

## Hors scope
Nouvelles fonctionnalités et composants, theming par cabinet, couleurs praticien/motif (T057).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** :
  - Pages migrées en daisyUI : `login`, `password_forgot`, `password_reset`, `invitation` (conteneur `max-w-md`, champs `space-y-1` + `input w-full`, `input-error` + `aria-invalid` + `aria-describedby` sur le login en échec, bouton `btn btn-primary w-full`), `admin_dashboard` (alerte `alert-info alert-soft`, listes `list-disc`), `admin_status` (table dans `overflow-x-auto`). `error.html` déjà en daisyUI (T081), variante admin : même gabarit, rien à changer.
  - Supprimés (`git rm`) : `static/{pico.min.css,tokens.css,components.css,base.css,app.css}`, `pages/dev_styleguide_v2.html`, `design/`, `poc/` (+ binaires non suivis de `poc/daisyui/.bin`, supprimés du disque). `static/` = `ui.css`, `app.js`, `agenda.js`, `htmx.min.js`, `vendor/`.
  - Go : `styleguide.go` réduit à la page v1 (tout le code `styleguideV2*` retiré), route `/dev/styleguide/v2` et ses 2 tests retirés, cas `dev_styleguide_v2` de `render_test.go` retiré, test des anciens CSS retiré de `styleguide_test.go` (ils n'existent plus ; le critère grep interdit aussi le mot « pico »), `admin_test.go` (`dl-field is-invalid` -> `input-error`), `calendar_test.go` (test « reste de la grille maison » retiré : contenait `dl-cal`).
  - `.claude/launch.json` : configs `design` et `poc-daisyui` retirées. `docs/design-system.md` : section « Héritage en cours de suppression » remplacée par « Fichiers statiques », référence à `design/tokens.css` retirée, `calendar_grid` retiré de la liste. `docs/README.md` : aucun lien vers `design/`/`poc/` (déjà propre depuis T080), inchangé.
  - CSP (`internal/httpx/middleware.go`) : déjà `default-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'`, jamais assouplie, inchangée. `input.css` : rien à retirer (aucune règle héritée ; `--fx-noise` neutralisé en T080). `{{asset ...}}` : seuls `ui.css`, `app.js`, `htmx.min.js` (base.html) et `vendor/event-calendar.min.{css,js}`, `agenda.js` (agenda) : tous existent.
- **Garde-fou Pico (éléments stylés par élément sans classe daisyUI)** : `button`, `select`, `textarea`, `table`, `dialog`, `article` : aucun sans `class=` (grep 2 vide). `input` sans classe : seulement les 2 champs honeypot `#website` (`public_details`, `public_waitlist`), masqués par leur conteneur `sr-only` (volontaire). `a[role=button]` : aucun. `details`/`summary`/`progress` sans classe : aucun. `nav ul` : seul `<ul class="steps">` (classé). `<a>` sans classe : le lien de marque du layout admin. `ul/ol/dl/hr/blockquote/small` sans classe : aucun (sinon puces perdues par le reset Tailwind).
- **Sorties des grep** (après `make css`) :
  - `grep -rniE 'pico|--app-|--dl-|dl-' internal/ docs/design-system.md AGENTS.md PLAN.md` (hors `ui.css`, qui n'en contient aucun non plus : `grep -c` = 0) : vide.
  - `grep -rnE '<(button|select|textarea|table|dialog|article)\b' internal/web/templates | grep -v 'class='` : vide.
  - `grep -rniE 'pico|--app-|--dl-' internal/ docs/design-system.md AGENTS.md PLAN.md` : vide.
  - `make css && git diff --exit-code ui.css` : `ui.css` régénéré (modifié par rapport à HEAD car des classes ont changé), `make check` (inclut `css-check`) vert, `golangci-lint` 0 issue, aucun `style=` dans les templates.
- **Taille du binaire** (`go build ./cmd/doctolibre`) : avant 26 562 114 o, après 26 362 242 o (-199 872 o : Pico, CSS maison et 5 fichiers embarqués en moins).
- **Contrôle navigateur** (base `doctolibre_demo`, port 8090, serveur redémarré sur le code final ; lecture seule sauf une déconnexion et une tentative de connexion avec un faux mot de passe) :
  - Méthode : pour chaque page, `fetch` + `document.write` dans l'onglet, puis, en clair et en sombre (`data-theme` posé à la main, 1 s d'attente pour les transitions), calcul du contraste AA de tout texte visible (ratio >= 4,5, >= 3 pour le grand texte) et de `scrollWidth` contre `innerWidth`. Pages : `/`, `/p/cabinet-demo`, fiche praticien, `/book?type=…`, `/p/cabinet-demo/privacy`, 404, `/admin/password/forgot`, `/admin/password/reset?token=x` (lien invalide), `/admin/invitation?token=x` (invalide), `/admin`, status, patients, patients/new, fiche patient, motifs, horaires, absences, paramètres, équipe, liste d'attente, confidentialité, journal, messages, rendez-vous (détail et nouveau), `/dev/styleguide`.
  - **1280 px** : toutes ces pages sauf `/dev/styleguide` : aucun texte sous AA, aucun débordement horizontal, clair et sombre. **360 px** : mêmes pages, aucun débordement ; contraste vérifié sans défaut sur toutes (les ratios de 1,0 à 2,5 relevés au premier passage sur des boutons étaient des transitions CSS en cours et ont disparu avec une attente plus longue ; seul `/dev/styleguide` signale un bouton `disabled` « Désactivé » à 1,5, exempt d'AA).
  - Agenda (vrai chargement) : 1280 px, semaine clair et sombre, « par praticien » à 4 praticiens sombre (colonnes lisibles, buffers hachurés), contraste sans défaut dans les deux thèmes ; 360 px jour sombre, `scrollWidth` 360, repli `#agenda-fallback` présent. Clavier : un RDV reçoit le focus (contour visible de 2 px, `:focus-visible` vrai après `Tab`), `Entrée` ouvre le dialogue (focus dans le dialogue), `Échap` le ferme et rend le focus au RDV. Le critère de T033 « vue jour à 4 praticiens à 1280 px » est vu.
  - Connexion : état « déconnecté » (flash) et échec de connexion à 360 px sombre : champs `input-error` bordés en rouge, alerte lisible, ordre de tabulation logique (lien d'évitement, marque, bascule de thème, email, mot de passe, bouton, lien d'oubli) ; clavier non testé page par page ailleurs.
  - Console : après un marqueur puis rechargement, aucune erreur ni violation CSP sur l'agenda (semaine, par praticien, jour). Le journal du volet conserve d'anciennes erreurs (`e.map is not a function`, logs `FE/DS/DC`) antérieures à ce passage (version précédente de `agenda.js` d'une session passée), non reproduites.
  - `style=` : 10 attributs sur l'agenda, posés par EventCalendar via le CSSOM (autorisés par la CSP) ; aucun dans les templates.
  - **Non vu** : états avec données (liste d'attente remplie, absences, invitations, pagination, doublons de patients), `public_confirmed`, `public_cancel`, `public_reschedule(_confirm)`, `public_verify`, `public_waitlist*`, `admin_care_plan_new` (aucun de ces écrans n'a été chargé : ils ont besoin d'un parcours ou d'un identifiant que la base demo ne fournit pas sans écrire), 1280 px du login, mode sombre à 360 px des pages publiques et admin hors login/agenda (mesuré par script, pas regardé à l'écran), navigation clavier hors agenda et login, piège de focus dans le dialogue (comportement natif de `showModal` supposé), dialogue d'agenda à 360 px, déplacer/annuler dans le dialogue. Le compte demo a été déconnecté et non reconnecté.
- **Défauts de STATUS.md** : « puce carrée Pico / contact à la ligne dans `patient_results` » : Pico supprimé, plus de puce ; `patient_results` non revu à l'écran (voir « non vu »), donc **levé pour Pico, à confirmer** pour le contact. « CSP bloque les icônes SVG `data:` de Pico » : sans objet (Pico supprimé, CSP inchangée). Le point « outillage front ... non testé sous Linux / CI » reste.
- **Reste à faire** : voir « Non vu » ci-dessus ; `ui_review.md` à la racine (suivi par git, hors périmètre) décrit encore Pico.
- **Écarts avec la spécification** :
  - `dev_styleguide_v2` retiré, mais aussi le test sur les anciens CSS de `styleguide_test.go` et un test de `calendar_test.go` (voir plus haut) : nécessaire pour que `grep 'pico|dl-'` soit vide.
  - `AGENTS.md`, `PLAN.md` : déjà sans occurrence de Pico, `--app-*`, `--dl-*`, `design/` ni `poc/` (nettoyés en T080) : rien à modifier. La mention de `docs/progress/` (STATUS) est laissée à l'orchestrateur.
