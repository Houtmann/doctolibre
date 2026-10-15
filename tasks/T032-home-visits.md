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
- [x] Le buffer de trajet bloque bien l'agenda (test moteur **et** test de contrainte en base).
- [x] Code postal hors zone refusé en réservation en ligne.

## Vérification
```bash
make check
```

## Hors scope
Optimisation de tournée, géocodage.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet).
- **Fait** : migration 00010 (appointment.address, patient.address, appointment_type.travel_buffer défaut 15 min, practice.home_visit_online + home_visit_zones) ; `domain.HomeBuffers` (règle unique trajet + marges du motif, utilisée par le moteur de créneaux, la réservation en ligne et la saisie cabinet) et `domain.InHomeZone` ; réservation en ligne : champ adresse (obligatoire, zone vérifiée dans StartVerification ET dans la transaction de Book, adresse liée au code OTP), motif à domicile masqué si option off ou zones vides ; saisie cabinet avec adresse ; page de confirmation et ICS (LOCATION = adresse) ; agenda : badge domicile, adresse + liens geo: et OpenStreetMap dans le détail ; admin : trajet par motif, case + zones dans les paramètres. Tests : domain, service (moteur + contrainte EXCLUDE + zone + staff), web (parcours public). `make check` vert.
- **Reste à faire** : rien dans le périmètre. L aide utilisateur (cumul 15 + 15 min) est documentée dans le formulaire des motifs ; pas de page d aide dédiée.
- **Notes de reprise** : sqlc : `go tool sqlc generate`. Les requêtes CreateAppointmentType/UpdateAppointmentType n ont pas été modifiées (fixtures partagées) : le trajet passe par `SetAppointmentTypeTravelBuffer`, les zones par `SetPracticeHomeVisit`.
- **Écarts avec la spécification** : (1) marges d un RDV à domicile = marges du motif + trajet (somme) plutôt que remplacées par le trajet ; identique tant que les marges du motif valent 0. (2) Plages dédiées au domicile : pas de nouveau code, `availability_rule.appointment_type_ids` suffit. (3) Le patient existant n est pas mis à jour avec l adresse (fiche jamais modifiée par une réservation en ligne) ; l adresse du RDV fait foi. (4) Zones : code postal ou nom de commune, comparés par mots entiers sans casse ni accents.
