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
- [ ] Deux RDV de praticiens différents ne peuvent pas occuper la même ressource en même temps (garanti par la base).
- [ ] Les créneaux proposés excluent les plages où la ressource est prise.
- [ ] Annulation et report libèrent la ressource.
- [ ] CRUD des ressources et liaison motif fonctionnent ; accès réservé admin (+ praticien si cohérent avec les motifs).
- [ ] Tests de concurrence et de DST ajoutés.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Capacité N (plateau technique, nécessiterait un verrou par ressource), plusieurs ressources possibles du même type par motif, ressources partagées entre cabinets.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
