# T032 — Visites à domicile

- **Phase** : 3
- **Dépend de** : T014
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Gérer les séances à domicile : adresse du patient, temps de trajet réservé autour du RDV, plages dédiées.

## Contexte à lire
`internal/booking/` (buffers), table `appointment_type` (`at_home`), `internal/service/booking.go`.

## Spécification
- Migration : `appointment.address text null`, `patient.address text null`, `appointment_type.travel_buffer interval default '15 minutes'`.
- Pour un motif `at_home` : l'adresse est obligatoire ; le trajet est copié dans les buffers du RDV (`buffer_before` = `buffer_after` = `travel_buffer`), donc protégé par la contrainte `EXCLUDE` de T010.
  Attention : deux visites à domicile consécutives cumulent leurs buffers (15 + 15 = 30 min entre les deux). Accepté en v1 comme marge de sécurité ; le documenter dans l'aide. Pas de calcul d'itinéraire en v1 (pas de dépendance à une API cartographique payante).
- Option : plages de disponibilité réservées au domicile (réutiliser `availability_rule.appointment_type_ids`).
- Agenda : icône « domicile » + adresse dans le détail, lien `geo:` / OpenStreetMap.
- Réservable en ligne seulement si le cabinet l'active, avec une liste de communes/codes postaux acceptés (`practice.home_visit_zones text[]`).

## Critères d'acceptation
- [ ] Le buffer de trajet bloque bien l'agenda (test moteur **et** test de contrainte en base).
- [ ] Code postal hors zone refusé en réservation en ligne.

## Vérification
```bash
make check
```

## Hors scope
Optimisation de tournée, géocodage.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
