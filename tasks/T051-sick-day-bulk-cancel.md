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
- [x] L'aperçu liste exactement les RDV non annulés de la plage, sans rien modifier.
- [x] La confirmation crée l'absence, annule les RDV, audite et enfile les messages dans une transaction ; un échec au milieu ne laisse rien.
- [x] Les messages contiennent le motif et le lien de T050.
- [x] Aucune offre de liste d'attente pour un créneau dans la plage fermée (test).
- [x] Accès refusé à un praticien sur l'agenda d'un autre.
- [x] Tests : service (aperçu, confirmation, atomicité, plage partielle avec heures, RDV déjà annulés ignorés), web (parcours).

## Vérification
```bash
make check
```

## Hors scope
Replanification automatique des séances de série, proposition automatique d'un nouveau créneau, ouverture au secrétariat.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-17 (sonnet).
- **Fait** :
  - `internal/service/availability.go` : validation extraite en `Practice.closureRange` (partagée par `AddClosure` et l'annulation groupée).
  - `internal/service/bulk_cancel.go` : `Calendar.CancelPreview` (lecture seule) et `Calendar.CancelRange` (une transaction `store.WithTx` : exception `closed` créée AVANT les annulations, puis `CancelAppointment`, `audit` par RDV (`meta.bulk=true`), message outbox via `notifier.cancelledFor` (motif + `manage_url` T050), ligne d'audit `availability.closure_cancel` sur l'exception ; événements émis après commit). Aucune nouvelle requête SQL ni migration (réutilise `ListAppointmentsForPractitioner`, `CancelAppointment`, `CreateAvailabilityException`).
  - `slotFreed` : inchangé. Comme `offerTo` revalide via `slotContext.check` (moteur de créneaux qui lit les exceptions via le `q` de la transaction), l'exception créée avant voit la plage fermée ; couvert par un test (témoin + cas fermé).
  - `internal/service/notifications.go` : `cancelledFor(..., reason)`, clé `reason` toujours présente dans `data`, motif tronqué à 20 caractères dans les SMS (`smsReasonMax`) ; `cancelled` délègue avec motif vide.
  - Templates `cancellation` (email html/txt, SMS) avec motif optionnel ; golden files régénérés (`-update`) ; test `TestCancellationReasonIsOptional`.
  - Web : `internal/web/admin_bulk_cancel.go` (GET `/admin/absences/cancel` = aperçu, POST = confirmation, 303 + flash ; erreurs de validation ré-affichées sur la page des absences en 422), page `admin_absences_cancel.html`, section « Absence imprévue » dans `absences_panel.html`, bouton « Absence imprévue » dans l'en-tête de l'agenda (absent pour le secrétariat). Routes sous `withTarget(ActionAvailabilityEdit)` : admin + praticien (remplaçant inclus via `Actor.Delegated`), pas le secrétariat. Sans JS (GET/POST classiques, pas de htmx).
  - Tests : `internal/service/bulk_cancel_test.go` (aperçu exact et sans effet, plage partielle avec heures, déjà annulés ignorés, audit, message avec motif et lien, atomicité par trigger d'échec, liste d'attente, accès, validation), `internal/web/admin_bulk_cancel_test.go` (parcours, secrétariat, praticien sur l'agenda d'un autre).
  - `ui.css` régénéré (`make tools` + `make css`). `make check` vert.
- **Reste à faire** : passage navigateur (page `/admin/absences`, aperçu, confirmation, bouton de l'agenda, 360 px) : non vu.
- **Notes de reprise** : RDV retenus = statut `booked`, soin intersectant `[from, to)` et début strictement après maintenant (un RDV déjà commencé n'est pas annulé ni notifié) ; plafond de 300 RDV (erreur de validation au-delà). Race théorique : un patient qui réserve entre l'aperçu et la confirmation sur un créneau de la plage n'est pas bloqué par une contrainte en base (la fermeture n'est pas un EXCLUDE) ; le RDV créé après la transaction n'est pas annulé.
- **Écarts avec la spécification** :
  - Le formulaire d'annulation groupée est distinct du formulaire d'ajout d'absence existant (champ « motif communiqué aux patients » ; le motif de l'absence simple reste « visible du cabinet uniquement »).
  - Texte SMS avec motif : « … annulé (motif) : lien » (sans « Reprendre ») pour tenir en 1 segment GSM-7 ; motif tronqué à 20 caractères dans le SMS, complet dans l'email.
  - `Calendar.Cancel` (annulation unitaire) n'envoie toujours pas de motif au patient (comportement inchangé).
