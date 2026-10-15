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
- [x] RDV réservé à J-0 pour le lendemain 9 h → rappel J-1 pas envoyé dans le passé (si l'heure est dépassée, envoyer tout de suite ou sauter — choisir et documenter).
- [x] Annulation → rappels `cancelled` (test).
- [x] Opt-out SMS respecté.
- [ ] Bout en bout en dev : réservation → mail visible dans Mailpit + SMS dans les logs.

## Vérification
```bash
make check
```

## Hors scope
Liste d'attente (T031).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet).
- **Fait** : migration 00012 (réglages `practice`, `appointment.verified_channel`, index outbox) ; templates email (confirmation, reminder, cancellation, reschedule, series) + SMS (`internal/notify/templates/sms`, `notify.RenderSMS`) avec golden files (`internal/notify/testdata`, `-update`) et test 1 segment GSM-7 ; messages enfilés DANS la transaction métier (`service/notifications.go`, appelé par Book, CancelByPatient, RescheduleByPatient, Calendar Create/Move/Cancel/SetStatus, CarePlans Create/AddSession) ; tâche périodique `service.Reminders` (`reminders.go`, lancée avec le worker dans `startWorker`, toutes les 5 min) ; page `/admin/settings/notifications` + estimation SMS/mois ; fusion de `main` (T030) : une seule confirmation « series » pour une série, rappels par séance. `make check` vert.
- **Reste à faire** : vérification manuelle bout en bout (Mailpit + logs SMS), non faite : critère 4 non coché. Un stash de sécurité `t023-wip-sonnet` (f9df21e) est à supprimer (stash drop), l'environnement m'en empêchait.
- **Notes de reprise** : le jeton de gestion n'est connu qu'à la création : les liens /r/{jeton} figurent dans la confirmation (et le déplacement fait par le patient) ; ils sont donc en clair dans `outbox_message.payload` (purge : T043). Jamais loggé. Les rappels et les messages du cabinet n'ont pas de lien (jeton haché, non récupérable). Les RDV créés par le cabinet reçoivent un jeton ; les séances d'une série n'en ont pas. Rappels : si l'heure d'envoi est passée, le rappel est enfilé tout de suite quand le RDV existait avant cette heure (rattrapage après panne), sinon sauté (RDV créé ou modifié après l'heure : `updated_at`) ; SMS décalé à 8 h (SMSRunAt) sauf si cela tombe à moins de 30 min du RDV (alors heure prévue). Les rappels d'un RDV déplacé sont annulés puis re-planifiés par la tâche. `reminder_series` s'applique aux RDV ayant un `care_plan_id`.
- **Écarts avec la spécification** : pas d'EventSink post-commit pour les messages (enfilement direct dans les services, demandé par l'orchestrateur) ; seul le canal vérifié par OTP reçoit des messages pour une réservation en ligne (confirmation par SMS si l'OTP est SMS, même avec un email saisi) ; l'OTP passait déjà par l'outbox (T014), inchangé, et le canal par défaut du formulaire reste SMS (le patient choisit) ; annulation par le patient : pas de message, rappels annulés ; estimation SMS : confirmations et rappels seulement.
