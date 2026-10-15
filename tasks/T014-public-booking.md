# T014 — Réservation en ligne patient

- **Phase** : 1
- **Dépend de** : T012, T013
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
Un patient, sans compte, réserve un créneau depuis la page publique du cabinet, sur mobile, en moins d'une minute.

## Contexte à lire
`internal/booking/` (API publique uniquement), `internal/service/practice.go`, `internal/web/routes.go`, layout public, tables `appointment`, `patient`.

## Spécification
Parcours (URL partageables, fonctionne sans JS, enrichi avec htmx) :
1. `GET /p/{practiceSlug}` : présentation cabinet + praticiens.
2. `GET /p/{practiceSlug}/{practitionerSlug}` : choix du motif (uniquement `online_bookable`).
3. `GET …/book?type={id}&week={yyyy-ww}` : créneaux par jour sur une semaine, navigation semaine suivante/précédente (htmx swap de la grille).
4. `GET/POST …/book/details?type=&start=` : formulaire nom, prénom, téléphone, email, date de naissance (optionnelle), consentements. Honeypot anti-bot.
5. Vérification d'identité : **code OTP 6 chiffres** envoyé par SMS ou email (pour l'instant via `notify.Log` — l'envoi réel arrive en T021/T022 ; prévoir une interface `OTPSender`). 5 essais max, validité 10 min, stocké hashé (table `otp_challenge` dans une migration de cette tâche).
6. `POST …/book/confirm` : transaction : retrouver ou créer le patient (match sur téléphone normalisé E.164 **ou** email + nom), insérer l'appointment en copiant les buffers du motif et en calculant `during_blocked` via `domain.BlockedRange`. Si `IsExclusionViolation` → retour à la grille avec message « Ce créneau vient d'être pris ».
7. Page de confirmation : récap, bouton « Ajouter à mon agenda » (.ics), lien de gestion (T015).

- `internal/service/booking.go` : `AvailableSlots(ctx, practitionerID, typeID, from, to)` (charge règles/exceptions/RDV puis appelle `booking.Slots`) et `Book(ctx, BookInput) (Appointment, error)`. **Revalider le créneau** côté serveur dans `Book` (ne jamais faire confiance au `start` reçu).
- Normalisation téléphone : `internal/domain/phone.go`, FR par défaut (`06…` → `+336…`), sans lib lourde.
- Rate limit : 10 recherches/min/IP, 3 OTP/heure/numéro.

## Critères d'acceptation
- [ ] Parcours complet testé (test d'intégration handler + DB).
- [ ] Test de concurrence : 2 `Book` simultanés sur le même créneau → 1 succès, 1 `ErrSlotTaken`.
- [ ] Un créneau hors dispo forgé dans l'URL est refusé.
- [ ] Rendu correct sur largeur 360 px.

## Vérification
```bash
make check
```

## Hors scope
Annulation/déplacement (T015), envoi réel SMS/email (T021–T023), domicile (T032).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
