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
- [ ] RDV annulé depuis ≤ 30 j : grille, confirmation, nouveau RDV avec jeton propre et message de confirmation, sans OTP (tests service DB + handler).
- [ ] Chaque garde-fou ci-dessus refuse, avec son test.
- [ ] POST rejoué : un seul nouveau RDV.
- [ ] Le bouton de `/r/{token}` et de l'accusé pointe vers la route de reprise quand elle est permise, sinon vers le repli (test handler).
- [ ] `make check` vert (dont `css-check` si un template change).

## Vérification
```bash
make css && make check && go test ./internal/service/ ./internal/web/
```

## Hors scope
- Reprise d'un RDV annulé par le cabinet pour un autre patient ; changement de praticien ou de motif.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** :
- **Fait** :
- **Reste à faire** :
- **Notes de reprise** :
- **Écarts avec la spécification** :
