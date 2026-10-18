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
- [x] Parcours complet testé (test d'intégration handler + DB).
- [x] Test de concurrence : 2 `Book` simultanés sur le même créneau → 1 succès, 1 `ErrSlotTaken`.
- [x] Un créneau hors dispo forgé dans l'URL est refusé.
- [ ] Rendu correct sur largeur 360 px.

## Vérification
```bash
make check
```

## Hors scope
Annulation/déplacement (T015), envoi réel SMS/email (T021–T023), domicile (T032).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet).
- **Fait** :
  - Migration `00008_otp_challenge.sql` : table `otp_challenge` (haché du code, haché du destinataire pour le rate limit, `binding_hash`) + index partiels `patient(practice_id, phone|email)`.
  - `domain.NormalizePhone` (E.164, FR par défaut) + tests ; `internal/ics` (un VEVENT, UTC, échappement, repliement à 75 octets) + tests ; templates email `otp.*` + test.
  - `service/booking.go` : `Practice`, `Practitioner`, `Target`, `AvailableSlots`, `CheckSlot`, `StartVerification`, `Book`, `Confirmation`. `service/otp.go` : `OTPSender`, `OutboxOTPSender` (SMS `otp:true` ou email `otp`), erreurs `ErrOTPWrong/Expired/RateLimited`. `service/patient.go` : validation de la saisie + `FindOrCreatePatient`.
  - Web : `internal/web/public.go` (`/p/{practice}`, `/p/{practice}/{practitioner}`, `…/book`, `…/book/details` GET/POST, `…/book/confirm`, `/r/{token}`, `/r/{token}/ics`), 6 pages + 3 partials, CSS, `Deps.Booking` branché dans `routes.go` et `serve.go`.
  - Tests : concurrence (2 `Book`, 1 succès + 1 `ErrSlotTaken`), créneaux forgés (service et HTTP), revalidation après envoi du code, tampering, 5 essais, expiration, rate limit 3/h, DST (22/03, 29/03, 25/10), motifs/plages, outbox, parcours HTTP complet, honeypot, CSRF, 429 par IP.
- **Reste à faire** : vérification visuelle à 360 px (non faite, voir notes) ; T015 branche les actions sur `/r/{token}`.
- **Notes de reprise** :
  - Flux : `StartVerification` (valide saisie + créneau, envoie le code dans une transaction avec l'outbox) puis `Book` (compte l'essai hors transaction, vérifie le `binding_hash`, puis transaction : consomme le code, revalide le créneau, trouve/crée le patient, `store.CreateAppointment`). Si la transaction échoue (`ErrSlotTaken`), la consommation du code est annulée avec elle.
  - Les données saisies voyagent en champs cachés entre « détails », « vérification » et « confirmation » ; `binding_hash` (créneau + canal + coordonnées + consentements) empêche de les modifier après l'envoi du code. Rien de personnel n'est stocké dans `otp_challenge`.
  - Rate limit 3 codes/heure/destinataire en base (verrou advisory sur le haché du destinataire) ; 10 requêtes/min/IP en mémoire (burst 10) sur la grille, le formulaire, l'envoi du code et la confirmation.
  - Tests : `DOCTOLIBRE_TEST_DATABASE_URL=postgres://doctolibre:doctolibre@127.0.0.1:5432/doctolibre_test?sslmode=disable make check`.
  - html/template échappe `+` et `'` dans le texte (`&#43;`, `&#39;`) : en tenir compte dans les assertions.
- **Écarts avec la spécification** (et pourquoi) :
  - Patient retrouvé par (téléphone, prénom, nom) ou (email, prénom, nom), et non par le téléphone seul : une famille partage un numéro, et un contact non vérifié (le canal choisi pour le code est seul vérifié) ne doit pas rattacher un rendez-vous à la fiche d'un tiers. Une fiche existante n'est jamais modifiée par une réservation en ligne.
  - Le lien de gestion est `/r/{token}` (convention de T015) : la page de confirmation y est servie, T015 y ajoutera les actions ; `.ics` fait en `internal/ics` (T015 le réutilise) sur `/r/{token}/ics`.
  - Le motif n'est proposé que s'il est actif, `online_bookable` et autorisé par au moins une plage (`practitioner_appointment_type` ignorée, décision de l'orchestrateur).
  - Case de consentement obligatoire ajoutée au formulaire, et canal du code (SMS/email) choisi par le patient.
  - Rendu 360 px non vérifié dans un navigateur (pas de serveur de démonstration avec données) : mise en page à une colonne, boutons `flex-wrap`, aucune largeur fixe.
