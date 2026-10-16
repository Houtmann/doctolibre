# T052 — Jours fériés français automatiques

- **Phase** : 5
- **Dépend de** : T013
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (temps)

## Objectif
Le cabinet peut fermer automatiquement les jours fériés français : le moteur de créneaux ne propose rien ces jours-là, sans saisir une absence par férié.

## Contexte à lire
`internal/booking/slots.go` (`Date`, `DateOf`, `Slots`, `Exception`, `Closed`/`Open`), `internal/booking/slots_test.go` (`TestSlotsDST`), `internal/service/booking.go` (construction de `booking.Request`), `internal/service/practice.go` (`SettingsInput`, `UpdateSettings`), `internal/web/admin_config.go` (`settingsInputOf`), `internal/web/templates/pages/admin_settings.html`, `internal/web/calendar.go` + `admin_calendar.html`, `AGENTS.md` (section Temps et fuseaux).

## Spécification
- Fonction pure `booking.FrenchHolidays(year int, alsaceMoselle bool) []booking.Date` (fichier `internal/booking/holidays.go`). 11 fériés : 1/1, lundi de Pâques, 1/5, 8/5, Ascension (Pâques + 39 j), lundi de Pentecôte (Pâques + 50 j), 14/7, 15/8, 1/11, 11/11, 25/12. Alsace-Moselle : + Vendredi saint (Pâques − 2 j) et 26/12. Résultat trié.
- Pâques : algorithme anonyme grégorien (Meeus/Jones/Butcher). Dates calendaires uniquement, pas de `time.Duration` en heures (`AddDate`).
- Migration 00018 (ou suivante si T050 l'a prise) : `practice.close_on_holidays boolean NOT NULL DEFAULT false`, `practice.holidays_alsace_moselle boolean NOT NULL DEFAULT false`. Cases dans `/admin/settings` (partie cabinet) ; `make sqlc`.
- Le moteur exclut ces jours (journée entière, fuseau du cabinet) si l'option est active ; une exception `open` explicite le même jour l'emporte. Implémentation suggérée : le service traduit les fériés de la fenêtre demandée en `booking.Exception{Kind: Closed}` (ou le moteur reçoit une liste de `Date` fermées) ; le choix reste cohérent avec la règle « `Closed` bloque aussi les créneaux dont les buffers débordent ».
- Les RDV existants ne sont pas touchés. L'agenda affiche « Férié » en en-tête de la journée concernée.
- Tests table-driven : Pâques 2024 (31/03), 2025 (20/04), 2026 (05/04), 2027 (28/03), 2038 (25/04) ; Ascension et Pentecôte dérivées ; Alsace-Moselle (Vendredi saint, 26/12) ; année de changement d'heure (31/03/2024 : dimanche de Pâques = jour du passage à l'heure d'été, vérifier le lundi 01/04) ; moteur : jour férié sans créneau, `open` explicite qui l'emporte, option désactivée = comportement inchangé.

## Critères d'acceptation
- [ ] `FrenchHolidays` correcte pour les années testées, avec et sans Alsace-Moselle.
- [ ] Avec l'option active, aucun créneau un jour férié (réservation en ligne et staff) ; une ouverture exceptionnelle le rouvre.
- [ ] L'agenda affiche « Férié ».
- [ ] Réglages enregistrables ; migration réversible.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Calendriers d'autres pays, ponts, fermeture automatique des RDV déjà pris.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
