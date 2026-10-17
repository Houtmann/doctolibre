# T060 — Ressources partagées (box, appareils)

- **Phase** : 5
- **Dépend de** : T013, T014, T016
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
Un motif peut exiger une ressource (box, appareil) : deux praticiens ne peuvent pas la réserver au même moment, ni en ligne ni au cabinet.

## Contexte à lire
`internal/booking/slots.go` (`Request`, `Busy`, `Slots`), `internal/booking/series.go`, `internal/service/booking.go` (`slotContext`, contrôle et insertion), `internal/service/calendar.go` (création, déplacement, `SetStatus`), `internal/service/manage.go` (report, annulation), `internal/service/care_plans.go`, `internal/store/appointment.go` (`MoveAppointment`, insertion), `migrations/00002_core.sql` (`EXCLUDE appointment_no_overlap` l. ~153), `internal/domain` (`ErrSlotTaken`), `internal/web/admin_config.go` + `types_panel.html` (CRUD motifs), `PLAN.md` §3.

## Spécification
**v1 : ressources de capacité 1 uniquement, une ressource précise par motif.**
- Tables : `resource(id, practice_id, name, active)` ; `appointment_type_resource(appointment_type_id, resource_id)` (au plus une ressource par motif en v1) ; `appointment_resource(appointment_id, resource_id, during_blocked tstzrange NOT NULL, active boolean NOT NULL DEFAULT true)` avec `EXCLUDE USING gist (resource_id WITH =, during_blocked WITH &&) WHERE (active)`. `active` passe à `false` dans **la même transaction** que l'annulation du RDV.
- `during_blocked` de la ressource = celui du RDV (buffers inclus, même règle que `appointment_no_overlap`).
- Moteur de créneaux : nouvelle entrée « occupations de ressource » (`[]booking.Busy` fournis par le service pour la ressource requise) ; un créneau n'est proposé que si la ressource est libre sur `during_blocked`. Fonction pure, tests sans DB.
- Tous les chemins de création/modification passent par le même contrôle et la même insertion : réservation en ligne, staff, report (patient et staff), séries, offres de liste d'attente. La violation de l'`EXCLUDE` de `appointment_resource` est traduite en `domain.ErrSlotTaken`. Déplacer un RDV met à jour `appointment_resource.during_blocked`.
- Admin : CRUD des ressources (nom, actif) et association à un motif dans le panneau des motifs. Désactiver une ressource ne touche pas aux RDV existants.
- Tests : concurrence (deux réservations simultanées de deux praticiens différents sur la même ressource → une seule passe), DST, annulation libère, report libère l'ancien créneau, créneaux non proposés quand la ressource est prise, motif sans ressource inchangé.
- `EXPLAIN` des nouvelles requêtes d'occupation dans Suivi (index sur `during_blocked`).

## Critères d'acceptation
- [x] Deux RDV de praticiens différents ne peuvent pas occuper la même ressource en même temps (garanti par la base).
- [x] Les créneaux proposés excluent les plages où la ressource est prise.
- [x] Annulation et report libèrent la ressource.
- [x] CRUD des ressources et liaison motif fonctionnent ; accès réservé admin (+ praticien si cohérent avec les motifs).
- [x] Tests de concurrence et de DST ajoutés.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Capacité N (plateau technique, nécessiterait un verrou par ressource), plusieurs ressources possibles du même type par motif, ressources partagées entre cabinets.

## Suivi

- **Sessions** : 2026-10-17 (sonnet).
- **Fait** : migration 00024 (`resource`, `appointment_type_resource` PK sur le motif, `appointment_resource` + `EXCLUDE appointment_resource_no_overlap WHERE active`) ; trigger `appointment_sync_resource` (INSERT : réserve la ressource active du motif ; UPDATE de `during_blocked`/`status` : suit le déplacement et libère à l'annulation) ; `store.isSlotConflict` traduit les deux contraintes en `ErrSlotTaken` (création et déplacement) ; moteur : `booking.Request.ResourceBusy` ; `slotContext.slots` charge `ListResourceBusyForType` (ignore le RDV déplacé) ; service `resources.go` (CRUD, liaison motif, contrôle de cabinet) ; UI : section « Ressources partagées » et select « Ressource exigée » dans le panneau des motifs ; `ui.css` régénéré. Tests : `booking/slots_resource_test.go` (dont DST mars/octobre), `service/resources_test.go` (concurrence réelle 2 praticiens, créneaux masqués, annulation, report, création/déplacement staff, done garde la ressource, inactive / sans ressource, tenancy), `web` `TestResourcesPanel`.
- **Reste à faire** : passage navigateur du panneau des motifs.
- **Notes de reprise** : tous les chemins d'écriture (en ligne, staff, report, séries, liste d'attente, annulation, SetStatus) passent par le trigger : aucune duplication côté Go. `SetStatus` ne peut pas sortir de `cancelled` (refusé en amont) ; le trigger gère quand même la réactivation (conflit => 23P01 => ErrSlotTaken). `done`/`no_show` gardent la ressource occupée.
- **Décisions** : ressource inactive = plus exigée pour les nouveaux RDV (lignes existantes conservées). Lier une ressource à un motif ne rétro-réserve pas les RDV futurs déjà pris de ce motif. Accès CRUD = celui des motifs (admin + praticien).
- **EXPLAIN** (200 000 lignes `appointment_resource`, 5 ressources, fenêtre de 3 jours, requête `ListResourceBusyForType`) : `Bitmap Index Scan on appointment_resource_no_overlap` (resource_id, during_blocked) ; 130 lignes, 0,3 ms ; l'index GiST de l'EXCLUDE sert la requête, pas d'index supplémentaire.
- **Écarts avec la spécification** : la synchronisation est faite par trigger SQL plutôt que par une fonction de service (garantie pour tous les chemins, y compris futurs).
