# T051 — Absence imprévue : annulation groupée

- **Phase** : 5
- **Dépend de** : T050
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
En une action, le praticien ferme une plage horaire et annule tous les RDV qui s'y trouvent ; chaque patient reçoit un message avec le motif et un lien pour reprendre RDV.

## Contexte à lire
`internal/service/availability.go` (`AddClosure`, `ClosureInput`, `configPractitioner`), `internal/web/admin_config.go` (`absencesPage`, `absencesAdd`, `absencesData`), `internal/web/templates/pages/admin_absences.html`, `internal/service/calendar.go` (annulation par le cabinet), `internal/service/notifications.go` (`cancelled`, `byStaff`), `internal/service/waitlist_offers.go` (`slotFreed`), `internal/service/audit.go`, `internal/auth/policy.go` (`ActionAvailabilityEdit`), `internal/domain/role.go` (`MayAccessPractitioner`).

## Spécification
- **Constat à respecter** : `AddClosure` ne fait que créer l'`availability_exception` `closed` ; elle **n'annule pas** les RDV existants. Réutiliser sa validation (dates/heures, fin dans le futur, motif ≤ 200 caractères) en l'extrayant dans une fonction partagée plutôt qu'en la dupliquant.
- Formulaire sur la page des absences (`/admin/absences`) et lien depuis l'agenda : praticien, début, fin, motif affiché au patient (ex. « Praticien souffrant »). Sans JS (POST + redirect), htmx en amélioration.
- **Étape 1 — aperçu** : liste des RDV non annulés touchés (heure, motif de RDV, nom du patient affiché dans la page, jamais dans les logs) et leur nombre.
- **Étape 2 — confirmation** : une seule transaction (`store.WithTx`) : crée l'exception `closed`, passe chaque RDV non annulé de la plage à `cancelled` (`cancel_reason` = motif), écrit l'`audit_log`, enfile les messages d'annulation dans la **même** transaction (motif + `manage_url` de T050). Les appels réseau restent hors transaction (outbox).
- **Piège** : `slotFreed` (liste d'attente) ne vérifie aujourd'hui que le préavis minimum du cabinet. Il ne doit **pas** proposer un créneau situé dans la fermeture qu'on vient de créer (créer l'exception avant les annulations dans la transaction et faire filtrer `slotFreed`, ou lui passer la plage). Test dédié.
- **Rôles** : `/admin/absences` est aujourd'hui réservé admin + praticien (`require` de `configRoutes`) et `ActionAvailabilityEdit` refuse le secrétariat. Garder cette règle (admin sur tout praticien, praticien sur son agenda, via `MayAccessPractitioner`) ; ouvrir l'annulation groupée au secrétariat n'est pas prévu : le signaler dans Suivi si besoin.
- Le message d'annulation existant (`TemplateCancellation`) doit accepter un motif optionnel ; adapter les templates email/SMS.

## Critères d'acceptation
- [ ] L'aperçu liste exactement les RDV non annulés de la plage, sans rien modifier.
- [ ] La confirmation crée l'absence, annule les RDV, audite et enfile les messages dans une transaction ; un échec au milieu ne laisse rien.
- [ ] Les messages contiennent le motif et le lien de T050.
- [ ] Aucune offre de liste d'attente pour un créneau dans la plage fermée (test).
- [ ] Accès refusé à un praticien sur l'agenda d'un autre.
- [ ] Tests : service (aperçu, confirmation, atomicité, plage partielle avec heures, RDV déjà annulés ignorés), web (parcours).

## Vérification
```bash
make check
```

## Hors scope
Replanification automatique des séances de série, proposition automatique d'un nouveau créneau, ouverture au secrétariat.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
