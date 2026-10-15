# T030 — Séries de séances kiné

- **Phase** : 3
- **Dépend de** : T016, T017
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Gérer la réalité kiné : une prescription = N séances (ex. 10 ou 15) à une fréquence donnée (ex. 2/semaine). Le praticien planifie la série en une fois et suit le décompte.

## Contexte à lire
`internal/booking/` (API), `internal/service/booking.go`, table `appointment` (`care_plan_id`), templates agenda + fiche patient.

## Spécification
- Migration : `care_plan(id, practice_id, patient_id, practitioner_id, appointment_type_id, label, sessions_prescribed int, frequency_per_week smallint, preferred_weekdays smallint[], preferred_time_window tstzrange null /* ou deux time */, start_date date, status text check in ('active','completed','cancelled'), created_at…)`.
  **Pas** de contenu médical (pas de pathologie, pas d'ordonnance scannée).
- Depuis la fiche patient : « Nouvelle série » → formulaire → **proposition automatique** de N créneaux selon fréquence et jours préférés, via `booking.Slots` (algo glouton : premier créneau compatible par semaine/jour préféré, en respectant un écart mini de 1 jour entre séances). Le praticien ajuste chaque ligne puis valide → création **atomique** de tous les RDV (tout ou rien, message clair si conflit).
- Fiche patient + détail RDV : badge « séance 4/10 ». Décompte = RDV `done` + `booked` non annulés.
- Quand une séance est annulée : proposer le prochain créneau compatible pour la remplacer.
- Côté patient (optionnel, setting cabinet) : réserver les séances suivantes d'une série depuis le lien de gestion.

## Critères d'acceptation
- [x] Planification de 10 séances 2×/semaine en un clic avec ajustement.
- [x] Test de l'algorithme de proposition (pur, table-driven) dans `internal/booking/series.go`.
- [x] Création atomique testée avec un conflit au milieu.

## Vérification
```bash
make check
```

## Hors scope
Facturation, suivi clinique.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** :
  - Migration `00013_care_plan.sql` (table `care_plan`, FK `appointment.care_plan_id`, index partiel), requêtes `care_plan.sql`, sqlc régénéré.
  - `booking.ProposeSeries` (`internal/booking/series.go`, pur) + tests table-driven (DST du 25/10/2026, jours préférés, repli, fenêtre horaire, remplacement).
  - `service.CarePlans` : `Form`, `Propose`, `Create` (atomique, `*LineConflict` avec la ligne en cause), `ForPatient`, `ForAppointment` (badge « séance n/N » + créneau de remplacement), `AddSession`, `SetStatus`. Marges via `slotContext.buffers()` (donc `domain.HomeBuffers`).
  - Web : `internal/web/care_plans.go` (formulaire, proposition, validation, statut, remplacement), page `admin_care_plan_new`, section « Séries de séances » sur la fiche patient, badge + bouton de remplacement dans le détail RDV.
  - Fusion de fiches patients : les séries suivent (`ReassignPatientCarePlans` dans `Patients.Merge`).
  - Tests : service (propose/create/décompte/remplacement, atomicité avec conflit au milieu, validations, fusion, statut), web (parcours complet, conflit, 403 secrétariat).
- **Reste à faire** : option « côté patient » (réserver les séances suivantes depuis le lien de gestion) non faite (marquée optionnelle) ; vérification visuelle 360 px du formulaire/tableau des séances non faite.
- **Notes de reprise** :
  - Algorithme : par semaine lundi-dimanche, quota = `frequency_per_week`, jours préférés d'abord puis repli sur les autres jours ; un créneau (le premier compatible) par jour ; écart minimal `MinGapDays` = 2 (un jour libre entre deux séances) si ≤ 3 séances/semaine, sinon 1. Fenêtre horaire = le soin doit tenir dans [début, fin].
  - Staff : `staffSlotContext` neutralise préavis et horizon du cabinet et accepte tous les motifs actifs (pas seulement « réservable en ligne »).
  - Remplacement : proposé à partir de demain (jamais avant `start_date`), en comptant les séances non annulées dans le quota et l'écart ; planifié via `POST /admin/care-plans/{id}/replace`.
  - `CarePlans.Events` est branché sur `NopSink` dans `serve.go` (comme `Calendar`) : T023 doit le remplacer par son sink pour que les séances de série notifient (événement `appointment.created` émis par séance après commit).
  - Routes réservées admin + praticien (la création utilise le praticien de l'utilisateur) ; le secrétariat voit les séries sur la fiche mais reçoit 403 sur les actions.
  - EXPLAIN : tables vides en base de test, plans non significatifs ; requêtes ciblées sur `appointment_care_plan_idx` (partiel), `appointment_patient_idx` et `care_plan_patient_idx`.
  - `TestCalendarWeekWith150AppointmentsIsFast` (seuil 100 ms) a échoué une fois à 107 ms sous charge pendant `make check`, vert au second passage (sans lien avec la tâche).
- **Écarts avec la spécification** :
  - `preferred_weekdays` : 0 = lundi … 6 = dimanche (comme `availability_rule`) ; fenêtre horaire en deux colonnes `time` (`preferred_time_start/end`) plutôt qu'un `tstzrange`.
  - « Écart mini de 1 jour » interprété comme un jour civil sans séance entre deux séances (diff ≥ 2 jours), assoupli à 1 jour au-delà de 3 séances/semaine.
  - Hors horaires refusé pour toutes les séances d'une série (pas de case « forcer »).
  - Annulation d'une série (`cancelled`) ne touche pas aux RDV déjà planifiés.
  - Option patient non implémentée.
