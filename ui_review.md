# Audit UI/UX de Doctolibre (Desktop & Mobile 360 px)

Après redéploiement de `doctolibre-app` (migration `00018` appliquée, conteneur `healthy`), voici les captures d'écran réalisées sur l'application (en mode sombre natif `prefers-color-scheme: dark` activé automatiquement par Pico CSS) et les problèmes visuels / ergonomiques identifiés.

---

## 1. Bugs de contraste critiques en Mode Sombre (`prefers-color-scheme: dark`)

> [!CAUTION]
> Pico CSS 2 bascule automatiquement en thème sombre selon l'OS de l'utilisateur (`prefers-color-scheme: dark`), mais [app.css](file:///Users/hadrien/Documents/doctolibre/internal/web/static/app.css#L2-L10) définit des couleurs de fond **claires en dur** (`#e8f0fe`, `#ececec`, `#f5f5f5`). En mode sombre, Pico force la couleur du texte des `<strong>`, `<li>` ou de la page en blanc/gris clair, ce qui rend certains blocs **illisibles**.

````carousel
![Tableau de bord Desktop (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/04_admin_dashboard_desktop.png)
<!-- slide -->
![Tableau de bord Mobile (360 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/05_admin_dashboard_mobile.png)
<!-- slide -->
![Agenda Semaine Desktop (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/06_admin_calendar_week_desktop.png)
<!-- slide -->
![Agenda Jour Desktop (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/07_admin_calendar_day_desktop.png)
````

### A. L'encadré `.callout` du tableau de bord est quasi illisible (contraste ~1,3:1)
- **Où** : [app.css](file:///Users/hadrien/Documents/doctolibre/internal/web/static/app.css#L383-L392) et [admin_dashboard.html](file:///Users/hadrien/Documents/doctolibre/internal/web/templates/pages/admin_dashboard.html#L10-L16).
- **Cause** : `.callout` applique `background: var(--app-info-bg)` (`#e8f0fe`, bleu très pâle) et `color: var(--app-info-fg)` (`#0b2f6b`), mais Pico CSS écrase la couleur de `<p>`, `<strong>` et `<li>` avec `var(--pico-color)` (gris très clair `#c2c7d0` en dark mode).
- **Correctif proposé** :
  - Soit forcer le thème clair sur `<html>` (`<html lang="fr" data-theme="light">` dans [base.html](file:///Users/hadrien/Documents/doctolibre/internal/web/templates/layout/base.html#L2)) pour avoir un rendu médical clair, constant et prévisible.
  - Soit définir une variante `@media (prefers-color-scheme: dark)` dans [app.css](file:///Users/hadrien/Documents/doctolibre/internal/web/static/app.css#L2-L10) + forcer `color: inherit` sur `.callout p, .callout strong, .callout li`.

### B. Les bandes « hors horaires » (`.cal-band`) de l'agenda éblouissent en mode sombre
- **Où** : [app.css](file:///Users/hadrien/Documents/doctolibre/internal/web/static/app.css#L123).
- **Cause** : `.cal-band` utilise `repeating-linear-gradient(45deg, #ececec, #ececec 6px, #f5f5f5 6px, #f5f5f5 12px)` en dur. En mode sombre, les plages **fermées** sont blanc vif tandis que les plages **ouvertes** sont bleu nuit (`#13171f`), ce qui inverse la hiérarchie visuelle (on croit que les zones blanches sont ouvertes !).
- **Correctif proposé** : Utiliser des variables CSS adaptées au thème (`--app-band-a`, `--app-band-b`) ou `rgba(128, 128, 128, 0.12)` / `rgba(128, 128, 128, 0.20)` qui fonctionnent aussi bien en thème clair qu'en thème sombre.

---

## 2. Navigation Admin & En-tête (`site-header`)

- **Où** : [admin.html](file:///Users/hadrien/Documents/doctolibre/internal/web/templates/layout/admin.html#L2-L29) et [app.css](file:///Users/hadrien/Documents/doctolibre/internal/web/static/app.css#L378-L381).
- **Problème visible sur toutes les captures Desktop (1280 px)** :
  - Le logo `Doctolibre` est tout seul sur une première ligne en haut à gauche, tandis que la liste des 9 liens + bouton `Se déconnecter` passe entièrement sur une seconde ligne décalée à droite.
  - Il n'y a **aucun indicateur de page active** (`aria-current="page"`) dans la barre de navigation principale : on ne voit pas d'un coup d'œil dans quelle rubrique on se trouve.
  - Comme noté dans [STATUS.md](file:///Users/hadrien/Documents/doctolibre/docs/progress/STATUS.md#L45), il manque un lien vers `/admin/status` (et `/admin/messages` est relégué en bas du tableau de bord).
- **Améliorations proposées** :
  1. Réduire légèrement le `padding` horizontal des `<li>` de la nav admin (`--pico-nav-element-spacing-horizontal: 0.5rem`) pour que `Doctolibre` + les entrées tiennent sur **une seule ligne** à 1280 px, avec une bordure basse (`border-bottom: 1px solid var(--pico-muted-border-color)`).
  2. Ajouter `aria-current="page"` (qui est stylé nativement par Pico CSS avec un soulignement primaire) sur l'onglet actif.
  3. Regrouper les pages techniques (`Messages envoyés`, `État de l'instance`, `Journal d'accès`) dans un sous-menu `<details class="dropdown">` « Système » ou dans le pied de page admin.

---

## 3. Agenda Praticien (Desktop & Mobile)

````carousel
![Agenda Mobile (360 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/08_admin_calendar_mobile.png)
<!-- slide -->
![Horaires d'ouverture (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/11_admin_availability_desktop.png)
````

1. **Barre d'outils de l'agenda (`.cal-toolbar`) sur 2 lignes mal équilibrées** ([calendar_grid.html](file:///Users/hadrien/Documents/doctolibre/internal/web/templates/partials/calendar_grid.html#L2-L18)) :
   - Sur `06_admin_calendar_week_desktop.png`, la barre se coupe en deux lignes (`Précédent | Aujourd'hui | Suivant` à gauche, le titre `Semaine du lundi 12 octobre au dimanche 18 octobre` à droite, puis `Jour | Semaine` en dessous à gauche et `Nouveau rendez-vous` à droite).
   - **Proposition** : Mettre les 4 blocs sur une seule barre compacte à 1280 px en réduisant la taille de `h1` (`font-size: 1.1rem; margin: 0 auto`) et en groupant `Précédent / Aujourd'hui / Suivant` avec un espacement `0.25rem`.
2. **Hauteur de la grille (2211 px de haut)** :
   - Avec `grid-auto-rows: 1.5rem` (24 px par tranche de 15 min, soit 96 px par heure de 07h à 21h), la grille fait ~1400 px de haut et oblige à scroller verticalement pour voir la fin d'après-midi.
   - **Proposition** : Passer `grid-auto-rows` à `1.15rem` (~18 px par 15 min, soit 72 px/heure) pour que toute la journée (8h–20h) tienne sur un écran 1080p sans scroll vertical, et rendre l'en-tête des jours (`.cal-head`) `position: sticky; top: 0; z-index: 5`.
3. **Vue mobile 360 px (`08_admin_calendar_mobile.png`)** :
   - En mode Semaine sur mobile, les 7 jours s'empilent avec `Aucun rendez-vous.` répété 6 fois.
   - **Proposition** : Masquer ou compacter sur une seule ligne grise les jours sans RDV en vue mobile, ou basculer par défaut sur la vue `Jour` quand l'écran est mobile.

---

## 4. Pages Patients, Équipe, Liste d'attente & Paramètres

````carousel
![Liste des patients (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/09_admin_patients_desktop.png)
<!-- slide -->
![Fiche patient (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/10_admin_patient_detail_desktop.png)
<!-- slide -->
![Paramètres du cabinet (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/12_admin_settings_desktop.png)
<!-- slide -->
![Liste d'attente (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/13_admin_waitlist_desktop.png)
<!-- slide -->
![Équipe du cabinet (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/14_admin_team_desktop.png)
<!-- slide -->
![État de l'instance (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/15_admin_status_desktop.png)
````

1. **Barre de recherche Patient (`09_admin_patients_desktop.png`) & Fusion (`10_admin_patient_detail_desktop.png`)** :
   - Dans [admin_patients.html](file:///Users/hadrien/Documents/doctolibre/internal/web/templates/pages/admin_patients.html#L5-L9), `<form role="search">` place le `<label>` **à l'intérieur** du groupe flex horizontal de Pico CSS (`role="search"` transforme le form en `display: inline-flex`). Résultat : le label « Rechercher (nom, prénom, téléphone ou email) » est écrasé à gauche de l'input sur deux lignes au lieu d'être au-dessus !
   - **Correctif** : Sortir le `<label>` du conteneur `role="search"` (ou mettre `role="search"` sur un `<div role="search">` contenant uniquement `<input>` + `<button>`).
2. **Tableau Équipe (`14_admin_team_desktop.png`)** :
   - Dans la colonne `Rôle` de [admin_team.html](file:///Users/hadrien/Documents/doctolibre/internal/web/templates/pages/admin_team.html#L17-L24), le `<select>` et le bouton `Changer` prennent chacun 100 % de la largeur et s'empilent verticalement, ce qui rend la ligne très haute.
   - **Correctif** : Utiliser `role="group"` de Pico CSS (ou `width: auto` sur `.inline-actions select, .inline-actions button`) pour que le `<select>` et `Changer` soient côte à côte sur une seule ligne compacte.
3. **Fiche Patient très longue (`10_admin_patient_detail_desktop.png`, 2382 px de haut)** :
   - Toutes les sections (Coordonnées, RDV à venir, Séries, Historique, RGPD, Fusionner un doublon) s'empilent sur une seule colonne pleine largeur.
   - **Correctif** :
     - Mettre les sections rares/destructrices (**Données personnelles RGPD** et **Fusionner un doublon**) dans des accordéons `<details><summary>…</summary>…</details>` fermés par défaut (natif HTML + stylé par Pico CSS, zéro JS).
     - Afficher **Coordonnées** à gauche et **Rendez-vous / Séries** à droite dans une `.grid` à 2 colonnes sur desktop.
4. **Cases à cocher des jours de la semaine (`11_admin_availability_desktop.png` & `13_admin_waitlist_desktop.png`)** :
   - Les 7 jours (`Lundi` … `Dimanche`) sont empilés verticalement sur 7 lignes, ce qui allonge inutilement les formulaires d'horaires et de liste d'attente.
   - **Correctif** : Ajouter une classe `.weekday-checks { display: flex; flex-wrap: wrap; gap: 0.5rem 1.25rem; }` sur le `<fieldset>` des jours pour les afficher sur **une seule ligne horizontale**.

---

## 5. Pages Publiques & Connexion

````carousel
![Connexion Admin (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/01_login_desktop.png)
<!-- slide -->
![Page publique du cabinet (1280 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/02_public_practice_desktop.png)
<!-- slide -->
![Page praticien Mobile (360 px)](/Users/hadrien/.gemini/antigravity/brain/fe6aeac9-21a5-48fd-a2ef-e1360db5bc53/03_public_practitioner_mobile.png)
````

1. **Page de connexion étirée sur toute la largeur (`01_login_desktop.png`)** :
   - Sur un écran 1280 px, les champs `Adresse email`, `Mot de passe` et le bouton `Se connecter` font ~1100 px de large.
   - **Correctif** : Envelopper le formulaire de connexion (et les pages publiques de réservation) dans un `<article class="auth-card">` centré avec `max-width: 28rem; margin-inline: auto;`.
2. **Page publique du cabinet (`02_public_practice_desktop.png`)** :
   - La carte praticien `Hadrien admin` est un bouton `outline` qui s'étire sur toute la largeur de l'écran.
   - **Correctif** : Limiter `.choice-list` à `max-width: 36rem` (ou afficher les praticiens en grille de cartes `<article>` avec leur titre, bio et un bouton « Prendre rendez-vous → »).
