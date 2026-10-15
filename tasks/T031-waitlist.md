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
- [ ] Deux patients notifiés cliquent → un seul obtient le créneau (test).
- [ ] Pas de notification pour un créneau libéré moins de `min_booking_notice` avant.

## Vérification
```bash
make check
```

## Hors scope
Attribution automatique sans clic.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
