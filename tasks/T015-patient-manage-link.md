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
- [ ] Annulation dans le délai OK, hors délai refusée.
- [ ] Déplacement atomique (test : nouveau créneau pris entre-temps → ancien RDV conservé).
- [ ] ICS validé (test golden file).

## Vérification
```bash
make check
```

## Hors scope
Envoi des messages (T023).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
