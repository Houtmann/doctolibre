# T100 — Retouches UX issues de l'essai Docker du 2026-11-11

- **Phase** : 5
- **Dépend de** : T072, T083, T095
- **Taille estimée** : M (< 500)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Corriger quatre défauts vus au navigateur lors de l'essai de bout en bout sur la pile Docker (journal du 2026-11-11) : téléphone affiché brut, focus intempestif dans le dialogue d'agenda, praticien affiché « admin » côté public, phrase de délai trompeuse dans l'email de confirmation.

## Contexte à lire
- `internal/web/render.go` (`funcMap`, l. ~493-520) et `internal/web/public.go` (`telHref`, l. ~698)
- `internal/domain/phone.go` (`NormalizePhone`)
- `internal/web/templates/partials/day_card.html`, `partials/appointment_detail.html`, `pages/admin_patients.html`, `pages/admin_patient.html`, `pages/public_practice.html`, `pages/public_booking_blocked.html`, `pages/public_rebook_closed.html`
- `internal/web/static/app.js` (l. ~46-60, ouverture du dialogue)
- `internal/service/practice.go` (`ensurePractitioner`), `internal/web/admin.go` (`DashboardBooking`, l. ~164-195), `pages/admin_dashboard.html`
- `internal/service/notifications.go` (`data()`, l. ~163-201), `internal/notify/templates/email/confirmation.{txt,html}.tmpl`, `internal/service/booking.go` (`Changeable`, l. ~646-656)

## Spécification

### 1. Téléphone lisible
- Ajouter `domain.FormatPhone(e164 string) string` : un numéro français `+33XXXXXXXXX` (9 chiffres après l'indicatif) devient `0X XX XX XX XX` ; tout autre numéro est rendu tel quel (pas d'erreur, pas de panique). Tests table-driven (mobile, fixe, `+32…`, vide, déjà formaté).
- Exposer `formatPhone` dans `funcMap` et l'utiliser **pour l'affichage** partout où un téléphone est montré : `day_card.html` (texte du bouton « Appeler … » ; le `href` reste `tel:{{telHref …}}`), `appointment_detail.html`, `admin_patients.html`, `admin_patient.html` (affichage, pas les `value` des champs de saisie), numéro du cabinet dans les pages publiques citées.
- Dans `appointment_detail.html`, le téléphone devient un lien `tel:` (même rendu que dans « Ma journée »).

### 2. Pas de focus sur un champ à l'ouverture du dialogue d'agenda
- À l'ouverture (`showModal()` dans `app.js`), le focus va sur le titre du dialogue (`tabindex="-1"` sur le titre, `.focus()` après `showModal()`), pas sur un champ de saisie : sur mobile le clavier ne doit pas s'ouvrir. Échap ferme toujours le dialogue et le focus revient à l'événement cliqué si EventCalendar le permet simplement (sinon ne pas insister).

### 3. Profil praticien à compléter
- `DashboardBooking` gagne `NeedProfile bool` : vrai si la fiche praticien de l'utilisateur a un prénom vide (cas des fiches créées par `ensurePractitioner` depuis la partie locale de l'email).
- L'encart « Pour que les patients puissent réserver » du tableau de bord l'affiche en premier : « Indiquez votre nom tel que les patients le verront » avec un lien vers `/admin/profile`. L'encart s'affiche dès qu'une des trois conditions est vraie. Rien pour le secrétariat (pas de fiche).
- Ne pas changer `ensurePractitioner` ni le comportement de `create-user`.

### 4. Phrase de délai exacte dans l'email de confirmation
- Dans `notifier.data`, n'envoyer `cancel_notice` que si le RDV est encore modifiable au moment de la rédaction (même règle que `Changeable` : `start - now >= MinCancelNotice`, avec l'horloge injectée). Sinon envoyer `cancel_closed` = durée formatée, et les templates (texte et HTML) disent « Ce rendez-vous ne peut plus être déplacé ni annulé en ligne (délai de 24 h) : contactez le cabinet. » (avec le téléphone du cabinet s'il est déjà dans les données).
- Factoriser la règle si c'est simple (une fonction pure dans `domain` ou `booking` utilisée par `booking.go`, `manage.go` et `notifications.go`) ; sinon au minimum un commentaire croisé.
- Mettre à jour les golden files (`-update`) et ajouter un cas golden « réservé dans les 24 h ».

## Critères d'acceptation
- [ ] « Ma journée », le dialogue d'agenda, la liste et la fiche patient affichent `06 12 34 56 78` pour `+33612345678` ; le lien `tel:` reste en E.164.
- [ ] Ouvrir un RDV dans l'agenda ne met le focus sur aucun champ de saisie.
- [ ] Un admin dont la fiche n'a pas de prénom voit l'étape « nom » dans l'encart du tableau de bord ; elle disparaît après enregistrement du profil.
- [ ] Un RDV pris à moins de `MinCancelNotice` reçoit un email sans « possibles jusqu'à … » et avec la phrase de délai dépassé ; un RDV plus lointain garde la phrase actuelle.
- [ ] Tests : `FormatPhone`, handler du tableau de bord (`NeedProfile`), golden emails des deux cas.
- [ ] `make css` relancé (templates modifiés), `ui.css` à jour.

## Vérification
```bash
make check
```

## Hors scope
- Changer le format de stockage des téléphones ou `NormalizePhone`.
- Renommer automatiquement la fiche praticien, modifier `create-user`.
- Les autres emails (rappels, déplacement) : seulement la confirmation.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
