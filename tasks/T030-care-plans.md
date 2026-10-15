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
- [ ] Planification de 10 séances 2×/semaine en un clic avec ajustement.
- [ ] Test de l'algorithme de proposition (pur, table-driven) dans `internal/booking/series.go`.
- [ ] Création atomique testée avec un conflit au milieu.

## Vérification
```bash
make check
```

## Hors scope
Facturation, suivi clinique.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
