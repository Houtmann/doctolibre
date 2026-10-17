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

## Critères d'acceptation
- [ ] Déplacement patient d'un RDV à domicile : trajet conservé dans `during_blocked` (test).
- [ ] Patient bloqué (T054) : `Claim` et `Join` en ligne refusés avec message clair ; cabinet non bloqué (tests service + handler).
- [ ] `Waitlist.Claim` émet l'événement de création et incrémente `AppointmentsCreated`.
- [ ] Une offre au plus par patient et par créneau libéré (test).
- [ ] Un RDV annulé ne repasse jamais en `booked` / `done` / `no_show` via `SetStatus` (test).
- [ ] Message de déplacement par le cabinet avec lien `/r/…` (test).
- [ ] Retirer de STATUS.md le « petit manque » sur le lien de déplacement (orchestrateur).
- [ ] `make check` vert.

## Vérification
```bash
make sqlc && make check
go test ./internal/service/ -run 'Reschedule|Waitlist|SetStatus|Move' -count=1
```

## Hors scope
Refonte des sinks d'événements (Nop aujourd'hui), contrôle d'accès des séries (T089), filtres `practice_id` de défense en profondeur (T091).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
