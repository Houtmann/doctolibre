# T015 — Gestion du RDV par le patient

- **Phase** : 1
- **Dépend de** : T014
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Via un lien personnel (envoyé par email/SMS), le patient consulte, annule ou déplace son RDV, dans le respect des délais du cabinet.

## Contexte à lire
`internal/service/booking.go`, `internal/auth/` (génération de tokens), table `appointment` (`manage_token_hash`).

## Spécification
- À la création du RDV : token aléatoire 32 octets (base64url) ; stocker `sha256` dans `manage_token_hash` ; lien `/{base}/r/{token}`.
- `GET /r/{token}` : récap + actions disponibles. Token inconnu → 404 générique (pas d'info sur l'existence).
- `POST /r/{token}/cancel` : refusé si `start - now < practice.min_cancel_notice` (message avec le téléphone du cabinet). Statut `cancelled`, `cancelled_by='patient'`.
- `GET /r/{token}/reschedule` → même grille que T014 (réutiliser les templates/partials) ; `POST` = transaction : annuler l'ancien + créer le nouveau (nouveau token), ou rien.
- `GET /r/{token}/ics` : fichier ICS du RDV (`internal/ics` minimal, RFC 5545, `\r\n`, échappements).
- Hooks pour les notifications (T023) : `service` publie des événements `AppointmentBooked`, `AppointmentCancelled`, `AppointmentRescheduled` via une interface `EventSink` (impl no-op pour l'instant).

## Critères d'acceptation
- [x] Annulation dans le délai OK, hors délai refusée.
- [x] Déplacement atomique (test : nouveau créneau pris entre-temps → ancien RDV conservé).
- [x] ICS validé (test golden file).

## Vérification
```bash
make check
```

## Hors scope
Envoi des messages (T023).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : `service/manage.go` (`CancelByPatient`, `RescheduleSlots`, `RescheduleByPatient`, verrou `FOR UPDATE` + délai `min_cancel_notice`, `ErrChangeTooLate`) ; `Booking.Events` (EventSink, nil = no-op) et événements `created`/`cancelled`/`moved` ; `Confirmation` enrichie (`Changeable`, `MinCancel`, `Horizon`) ; `slotContext.ignore` pour que le RDV déplacé ne bloque pas ses propres créneaux ; routes `GET|POST /r/{token}/cancel`, `GET /r/{token}/reschedule` (+ `/confirm`), `POST /r/{token}/reschedule`, toutes limitées par IP ; pages `public_cancel`, `public_reschedule` (réutilise `booking_grid`), `public_reschedule_confirm` ; boutons sur `public_confirmed` ; ICS déjà fait en T014, ajout d'un golden file (`internal/ics/testdata/appointment.ics`, `-update`). Tests : `service/manage_test.go`, `web/public_manage_test.go`.
- **Reste à faire** : vérification visuelle 360 px non faite ; branchement réel des notifications (T023).
- **Notes de reprise** : tests DB avec `DOCTOLIBRE_TEST_DATABASE_URL=postgres://hadrien@127.0.0.1:5432/doctolibre_test?sslmode=disable`. Aucune migration ajoutée.
- **Écarts avec la spécification** (et pourquoi) : le déplacement se fait en place (`store.MoveAppointment`, demandé par l'orchestrateur) au lieu de « annuler + recréer » ; le jeton n'est donc pas renouvelé et le lien reçu reste valable. Le délai `min_cancel_notice` s'applique aussi au déplacement. Pas d'`audit_log` pour les actions patient (pas d'acteur). Un RDV dont le motif n'est plus réservable en ligne ne peut pas être déplacé par le patient (message + téléphone du cabinet). Les événements existants (`EventAppointmentCreated/Moved/Cancelled`) sont réutilisés au lieu de `AppointmentBooked/Rescheduled`.
