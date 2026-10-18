# T031 — Liste d'attente

- **Phase** : 3
- **Dépend de** : T023
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Quand un créneau se libère, les patients en liste d'attente compatibles sont prévenus ; le premier qui clique l'obtient.

## Contexte à lire
`internal/service/notifications.go`, `internal/outbox/`, `internal/service/booking.go`.

## Spécification
- Migration : `waitlist_entry(id, practice_id, patient_id, practitioner_id null, appointment_type_id, earliest date, latest date, weekdays smallint[], time_from time, time_to time, status text check in ('active','fulfilled','expired','cancelled'), created_at)`.
- Inscription : côté patient (bouton « Aucun créneau ne me convient » sur la grille T014, OTP requis) et côté cabinet.
- Sur `AppointmentCancelled` : chercher les entrées compatibles (ordre FIFO, max 5), envoyer un SMS/email avec lien `/w/{token}` valable 2 h.
  Clic → si créneau toujours libre, réservation (même code que T014 `Book`) ; sinon « déjà pris ».
- Expiration automatique des entrées après `latest`.
- Écran `/admin/waitlist`.

## Critères d'acceptation
- [x] Deux patients notifiés cliquent → un seul obtient le créneau (test).
- [x] Pas de notification pour un créneau libéré moins de `min_booking_notice` avant.

## Vérification
```bash
make check
```

## Hors scope
Attribution automatique sans clic.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : migration `00014_waitlist.sql` (`waitlist_entry` + `waitlist_offer`, jeton haché) ; requêtes `waitlist.sql` ; service `Waitlist` (`internal/service/waitlist.go` : inscription patient avec OTP, inscription cabinet, liste/retrait, `Offer`/`Claim` du lien `/w/{jeton}`, `ExpireOnce`/`Run`) ; envoi des propositions dans la transaction métier (`waitlist_offers.go`, branché dans `notifier.cancelled` et `notifier.rescheduled`, donc annulation patient, annulation/déplacement cabinet et déplacement patient) ; templates `waitlist` (email sujet/txt/html + SMS) + golden files ; web : bouton « Aucun créneau ne me convient » sous la grille, formulaire + code + inscription (`/p/{cabinet}/{praticien}/waitlist`), page `/w/{jeton}` (GET lecture seule, POST réserve), écran `/admin/waitlist` (+ lien dans la nav) ; expiration horaire dans le worker ; tests service, web, templates.
- **Reste à faire** : rien de bloquant. Vérification visuelle (360 px) non faite.
- **Notes de reprise** : le moteur de créneaux (`slotContext.check`) a le dernier mot à l'enfilement et au clic ; la contrainte EXCLUDE tranche la course (`domain.ErrSlotTaken`, HTTP 409 sur POST /w). `min_booking_notice` est donc respecté par construction (en plus d'un court-circuit explicite). Les propositions exigent `BaseURL` (sinon aucune proposition, pas de lien). Tests DB : `DOCTOLIBRE_TEST_DATABASE_URL='postgres://doctolibre:doctolibre@127.0.0.1:5432/doctolibre_test?sslmode=disable'`. `sqlc` ne gère pas `AT TIME ZONE` dans `@param::timestamptz AT TIME ZONE x` : utiliser `timezone(x, ...)`.
- **Écarts avec la spécification** (et pourquoi) :
  - Pas d'`EventSink` sur `AppointmentCancelled` : les propositions sont enfilées dans la transaction métier (consigne de l'orchestrateur).
  - `waitlist_entry` contient en plus `verified_channel` et `source` ; table `waitlist_offer` ajoutée (une ligne par patient prévenu et par créneau, jeton haché, 2 h à compter de l'envoi réel, SMS reportés de nuit compris).
  - Visites à domicile exclues de la liste d'attente (adresse non gérée) ; 5 inscriptions actives max par patient.
  - Côté cabinet, l'inscription se fait par saisie des coordonnées (patient retrouvé ou créé), sans recherche de patient.
  - Test `TestMigrateUpDownUp` : plafond de la boucle de descente relevé de 10 à 25 (les migrations s'accumulent).
