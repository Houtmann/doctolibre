# T088 — Corrections RDV : trajet au déplacement, liste d'attente, statut concurrent

- **Phase** : 5
- **Dépend de** : T032, T054, T031, T050
- **Taille estimée** : M (< 500)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (concurrence, invariant de non-chevauchement)

## Objectif
Cinq bugs confirmés par l'audit du 2026-10-17 (vérifiés dans le code) sont corrigés, chacun avec son test de non-régression : le déplacement par le patient conserve le temps de trajet, la liste d'attente respecte le garde-fou d'absences et n'envoie qu'une offre par patient, un changement de statut ne ressuscite plus un RDV annulé, et le message de déplacement envoyé par le cabinet contient le lien de gestion.

## Contexte à lire
- `internal/service/manage.go` (`RescheduleByPatient`, ~l. 136), `internal/service/booking.go` (`slotContext.buffers`, `onlineBookingBlocked`, `Book`), `internal/store/appointment_move.go`
- `internal/service/waitlist.go` (`Join`, `Claim`, `maxActiveWaitlist`), `internal/service/waitlist_offers.go` (`slotFreed` / `OfferForFreedSlot`, `waitlistMaxOffers`), `internal/store/queries/waitlist.sql` (`ListWaitlistCandidates`)
- `internal/service/calendar.go` (`SetStatus` ~l. 537, `Move` ~l. 470), `internal/store/queries/appointment.sql` (`SetAppointmentStatus`), `internal/service/manage_token.go` (`ManageKey.forAppointment`), `internal/service/notifications.go` (`rescheduled`)
- `internal/web/public_waitlist.go` (mappage des erreurs de `/w/{token}`), `cmd/doctolibre/serve.go` (câblage `Events`, `OnCreated`)

## Spécification

1. **Trajet perdu au déplacement patient (critique)** — `RescheduleByPatient` passe à `store.MoveAppointment` les marges brutes du motif (`sc.typ.BufferBefore/After`) : pour un motif à domicile, `during_blocked` perd `TravelBuffer` alors que la recherche de créneaux l'inclut. Utiliser `bb, ba := sc.buffers()`. Test : motif à domicile (trajet > 0), déplacement patient → `during_blocked` = soin ± (marge + trajet) ; un RDV posé dans le temps de trajet est refusé (`ErrSlotTaken`).
2. **Garde-fou T054 contourné par la liste d'attente (critique)** — `Waitlist.Claim` crée un RDV `Source: "online"` sans `onlineBookingBlocked`. L'appeler dans la transaction de `Claim` (→ `domain.ErrOnlineBookingBlocked`) et dans `Join` pour l'inscription en ligne (pas pour `AddByStaff`). Le handler `/w/{token}` et l'inscription publique affichent le même message que la réservation bloquée (inviter à appeler le cabinet). Ajouter à `Waitlist` les champs `Events EventSink` et `OnCreated` (métrique `AppointmentsCreated`), câblés dans `serve.go` comme `Booking`, et les appeler après commit. Tests : patient au-dessus du seuil → `Claim` et `Join` en ligne refusés, inscription par le cabinet acceptée.
3. **Plusieurs offres au même patient** — `ListWaitlistCandidates` ne déduplique pas par patient (jusqu'à 5 inscriptions actives) : un patient peut recevoir jusqu'à 5 messages pour le même créneau et occuper toutes les offres. Dédupliquer en SQL (inscription la plus ancienne par `patient_id`, ordre final inchangé `created_at, id`) puis `make sqlc`. Test : patient avec 2 inscriptions compatibles + 1 autre patient → 2 offres, une par patient.
4. **`SetStatus` peut écraser une annulation concurrente** — `Calendar.SetStatus` lit sans verrou puis `SetAppointmentStatus` met à jour sans condition sur le statut. Ajouter `AND status <> 'cancelled'` au `WHERE` (requête `:one`/`:execrows`) et traduire « 0 ligne » en `ErrValidation` (« RDV annulé entre-temps ») ; garder le test Go existant pour le message lisible. Test : RDV annulé après la lecture (appel direct de la requête) → aucune écriture, erreur renvoyée.
5. **Déplacement par le cabinet sans lien de gestion** — `Calendar.Move` passe `""` (commentaire périmé depuis T050). Corriger dans `notifier.rescheduled` : si `manageToken == ""`, utiliser `n.key.forAppointment(appt)` (repli sans lien conservé pour les anciens jetons aléatoires). Supprimer le commentaire périmé. Test : message enfilé après `Calendar.Move` contient `/r/`.

6. **Titre de la page d'un RDV annulé** (passage navigateur du 2026-10-17) — `GET /r/{token}` d'un RDV annulé affiche « Rendez-vous annulé » mais le `<title>` reste « Rendez-vous confirmé ». Aligner le titre sur l'état (annulé / confirmé). Test handler : le `<title>` d'un RDV annulé contient « annulé ».
7. **Libellé du refus T054** (passage navigateur du 2026-10-17) — le refus de réservation en ligne s'affiche sous « Le formulaire contient des erreurs : », ce qui invite le patient à corriger et réessayer. Le rendre comme une alerte à part (sans le titre d'erreurs de formulaire), même texte (« … appeler le cabinet au … »). Même rendu pour les refus liste d'attente du point 2. Test handler : la page de refus ne contient pas « Le formulaire contient des erreurs ».

## Critères d'acceptation
- [x] Déplacement patient d'un RDV à domicile : trajet conservé dans `during_blocked` (test).
- [x] Patient bloqué (T054) : `Claim` et `Join` en ligne refusés avec message clair ; cabinet non bloqué (tests service + handler).
- [x] `Waitlist.Claim` émet l'événement de création et incrémente `AppointmentsCreated`.
- [x] Une offre au plus par patient et par créneau libéré (test).
- [x] Un RDV annulé ne repasse jamais en `booked` / `done` / `no_show` via `SetStatus` (test).
- [x] Message de déplacement par le cabinet avec lien `/r/…` (test).
- [x] `<title>` d'un RDV annulé cohérent ; refus T054 hors du bloc « erreurs du formulaire » (tests).
- [ ] Retirer de STATUS.md le « petit manque » sur le lien de déplacement (orchestrateur).
- [x] `make check` vert.

## Vérification
```bash
make sqlc && make check
go test ./internal/service/ -run 'Reschedule|Waitlist|SetStatus|Move' -count=1
```

## Hors scope
Refonte des sinks d'événements (Nop aujourd'hui), contrôle d'accès des séries (T089), filtres `practice_id` de défense en profondeur (T091).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-17 (sonnet).
- **Fait** : points 1 à 7. (1) `RescheduleByPatient` utilise `sc.buffers()`. (2) `onlineBookingBlocked` dans la transaction de `Claim` et de `Join` (pas `AddByStaff`) ; `Waitlist.Events`/`OnCreated` ajoutés, appelés après commit, câblés dans `serve.go` (`OnCreated` avec la métrique). (3) `ListWaitlistCandidates` : `DISTINCT ON (patient_id)` (inscription la plus ancienne) puis ordre `created_at, id` ; le `NOT EXISTS` joint les inscriptions du même patient (un patient déjà offert via une autre inscription n'est pas re-sollicité). (4) `AND status <> 'cancelled'` dans le WHERE de `SetAppointmentStatus`, `ErrNoRows` -> `ErrValidation` ; `internal/store/schema_test.go` réactive désormais l'annulé en SQL direct (la requête applicative ne le permet plus). (5) `notifier.rescheduled` dérive le jeton via `n.key.forAppointment` si vide. (6) `<title>` de `public_confirmed.html` selon l'état. (7) refus T054 (Book, Join, Claim) rendu par `rd.Error(403, ...)` (page d'erreur avec le message, hors bloc « erreurs du formulaire »). `make sqlc`, `make css` (ui.css inchangé en pratique ou régénéré), `make check` vert.
- **Tests** : `internal/service/t088_test.go` (trajet conservé, garde-fou Join/Claim/AddByStaff + événement/métrique, une offre par patient, SetAppointmentStatus sur annulé) ; assertion `/r/` ajoutée à `TestStaffAppointmentNotifications` ; web : `<title>` annulé, refus sans « Le formulaire contient des erreurs » (réservation et liste d'attente).
- **EXPLAIN** : `ListWaitlistCandidates` filtre sur `practice_id` + `status='active'` (index partiel `waitlist_entry_active_idx (practice_id, created_at, id)`), puis tri par `patient_id` sur les seules inscriptions actives du cabinet (petit volume) ; pas d'EXPLAIN sur données réelles. `SetAppointmentStatus` reste adressée par PK.
- **Reste à faire** : rien. Non vu au navigateur : page de refus 403 (titre « Accès refusé » fourni par `Renderer.Error`), refus au clic sur un lien `/w/{token}` (pas de test handler pour `Claim` bloqué).
- **Notes de reprise** : tests DB avec `DOCTOLIBRE_TEST_DATABASE_URL`.
- **Écarts avec la spécification** : le refus T054 est une page d'erreur 403 complète (on perd le formulaire pré-rempli, inutile puisque réessayer ne sert à rien) ; titre de page « Accès refusé » à affiner si besoin. `serve.go` : `Events: NopSink{}` sur `Waitlist`.
