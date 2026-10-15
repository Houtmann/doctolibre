# T023 — Confirmations et rappels

- **Phase** : 2
- **Dépend de** : T021, T022, T015
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le patient reçoit automatiquement confirmation, rappels et notifications d'annulation/déplacement, par email et/ou SMS selon ses préférences et la config du cabinet. Les OTP passent par le vrai canal.

## Contexte à lire
`internal/service/` (interface `EventSink`), `internal/outbox/outbox.go`, templates `internal/notify/templates/`, `docs/sms.md` (section « Réglages par défaut retenus »).

## Spécification
- Implémenter `EventSink` → `internal/service/notifications.go` : dans **la même transaction** que l'action métier :
  - `AppointmentBooked` → confirmation immédiate (canal selon réglages) + rappel(s) planifiés.
  - `AppointmentCancelled` → `CancelForAppointment(<id>)` + message d'annulation (si annulé par le cabinet).
  - `AppointmentRescheduled` → annuler anciens rappels, nouveau message + nouveaux rappels.
- Réglages cabinet (`/admin/settings/notifications`, colonnes ajoutées à `practice`) : canal par type de message, rappel J-1 (heure d'envoi, ex. 18:00 la veille), rappel H-2 (on/off), rappel pour les séances d'une série en cours (on/off, utilisé par T030), signature.
  **Valeurs par défaut** (cf. `docs/sms.md`, objectif coût) : confirmation par **email** (SMS seulement si pas d'email), rappel J-1 par **SMS**, H-2 désactivé, OTP par email si une adresse est fournie sinon SMS.
- Afficher dans les réglages une **estimation du nombre de SMS/mois** (moyenne des 4 dernières semaines × réglages actuels) pour que le praticien voie l'impact de ses choix.
- Templates FR : `confirmation`, `reminder`, `cancellation`, `reschedule`, `otp`, chacun en email (txt+html) et SMS (≤ 160 car. GSM-7 visé, test qui le vérifie avec données réalistes). Inclure le lien de gestion (T015) et l'adresse.
- Tout message lié à un RDV renseigne `appointment_id` ; `dedup_key` systématique (`reminder-24h:<appointment_id>`) → idempotence.
- Remplacer l'`OTPSender` log de T014 par l'envoi direct (OTP = priorité, pas de fenêtre horaire).

## Critères d'acceptation
- [ ] RDV réservé à J-0 pour le lendemain 9 h → rappel J-1 pas envoyé dans le passé (si l'heure est dépassée, envoyer tout de suite ou sauter — choisir et documenter).
- [ ] Annulation → rappels `cancelled` (test).
- [ ] Opt-out SMS respecté.
- [ ] Bout en bout en dev : réservation → mail visible dans Mailpit + SMS dans les logs.

## Vérification
```bash
make check
```

## Hors scope
Liste d'attente (T031).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
