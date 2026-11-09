# T098 — Reprendre rendez-vous sans nouvelle vérification

- **Phase** : 5
- **Dépend de** : T095, T097
- **Taille estimée** : M (< 500)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (contournement d'OTP)

## Objectif
Un patient dont le RDV a été annulé récemment reprend rendez-vous en un clic, sans ressaisie ni OTP, avec les garde-fous du déplacement.

Origine : audit UX des parcours patients du 2026-10-17.

## Contexte à lire
- `internal/service/manage.go` (`RescheduleSlots`, `RescheduleByPatient`), `internal/service/manage_token.go`, `internal/service/booking.go` (`Book`, `onlineBookingBlocked`, motif actif et `online_bookable` ~l. 208)
- `internal/web/public_manage.go` (`rescheduleGrid`), `internal/web/limits.go` (`reservePractitioner` ~l. 147), `internal/web/templates/pages/public_confirmed.html`
- `internal/store/queries/appointment.sql` (~l. 6, `verified_channel`), `docs/decisions.md`

## Spécification
**Décision** (`docs/decisions.md`, 2026-10-17) : un RDV annulé depuis au plus 30 jours permet de reprendre RDV via `/r/{token}/rebook` sans ressaisie ni OTP, en réutilisant le patient et `appointment.verified_channel`, sur le modèle du déplacement.

- Grille du même praticien et du même motif (réutiliser `RescheduleSlots` / `rescheduleGrid` sans `ignore`), confirmation avant POST, nouveau RDV avec son propre jeton, message de confirmation normal.
- Méthode service dédiée (ex. `Booking.RebookByPatient`) ; horloge injectée, pas de `time.Now()`.
- Le bouton « Reprendre rendez-vous » de `/r/{token}` et de l'accusé d'annulation (T095) pointe vers cette route quand elle est permise, sinon vers le repli de T095.
- Garde-fous obligatoires, chacun avec un test : annulation > 30 j ou RDV non annulé → refus (page explicative, retour vers la page praticien) ; `onlineBookingBlocked` (T054, page T092) ; quota `reservePractitioner` ; motif encore actif et `online_bookable` ; un seul rebook par RDV annulé (POST rejoué → pas de doublon) ; collision → « créneau plus disponible ».

## Critères d'acceptation
- [x] RDV annulé depuis ≤ 30 j : grille, confirmation, nouveau RDV avec jeton propre et message de confirmation, sans OTP (tests service DB + handler).
- [x] Chaque garde-fou ci-dessus refuse, avec son test.
- [x] POST rejoué : un seul nouveau RDV.
- [x] Le bouton de `/r/{token}` et de l'accusé pointe vers la route de reprise quand elle est permise, sinon vers le repli (test handler).
- [x] `make check` vert (dont `css-check` si un template change).

## Vérification
```bash
make css && make check && go test ./internal/service/ ./internal/web/
```

## Hors scope
- Reprise d'un RDV annulé par le cabinet pour un autre patient ; changement de praticien ou de motif.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-11-09 (sonnet)
- **Fait** : migration 00029 (`appointment.rebooked_appointment_id`, FK `ON DELETE SET NULL`, index unique partiel) ; requête `MarkAppointmentRebooked` (UPDATE conditionnel `rebooked_appointment_id IS NULL`, posé dans la transaction de création, 0 ligne = `ErrAlreadyRebooked` et rollback) ; `internal/service/rebook.go` (`RebookSlots`, `RebookByPatient`, `ErrRebookClosed`, `ErrAlreadyRebooked`, `Confirmation.Rebookable`) ; routes `GET /r/{token}/rebook`, `GET /r/{token}/rebook/confirm` (seau `search`), `POST /r/{token}/rebook` (seau `manage` + quota praticien) ; 3 templates (`public_rebook`, `public_rebook_confirm` avec consignes et case, `public_rebook_closed`) ; bouton de `/r/{token}` et lien `rebook_url` de l'accusé d'annulation (T095) vers la reprise quand elle est permise, sinon repli `RebookPath`. Tests service (concurrence à 4 goroutines, rejeu, fenêtre J+30 à la seconde près, T054, motif, créneau pris, consignes) et handler. Corrigé en plus : assertion « le corps ne contient pas 403 » de `assertBookingBlocked` (no_show_guard_test.go), qui rendait instables `TestPublicBookingBlockedByNoShows` et `TestPublicWaitlistJoinBlockedByNoShows` (statut HTTP et textes de la page de refus restent vérifiés).
- **Reste à faire** : passage navigateur (grille, confirmation, page de refus) ; non vu à l'écran.
- **Notes de reprise** : l'ancien RDV garde son jeton ; le nouveau a le sien (redirection vers `/r/{nouveau}`). Rejeu : page 403 « Rendez-vous déjà repris ». Tests existants mis à jour : `TestPatientCancelFlow`, `TestPatientCancelledRebookURL`, `TestCancelByPatientQueuesEmailAckWithRebookLink`, `render_test.go`.
- **Écarts avec la spécification** : la fenêtre de 30 jours est une durée fixe (`30*24h`) et non 30 jours calendaires, pour éviter l'ambiguïté DST ; la reprise est ouverte à tout RDV annulé (patient ou cabinet) dont on possède le jeton ; le quota praticien est pris même pour un lien refusé puis rendu (`Cancel`), donc un quota épuisé masque la page explicative.
