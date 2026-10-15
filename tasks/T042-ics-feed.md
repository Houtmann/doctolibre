# T042 — Flux ICS

- **Phase** : 4
- **Dépend de** : T016
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le praticien s'abonne à son agenda depuis son téléphone (Google Agenda, Apple Calendrier, Thunderbird) en lecture seule.

## Contexte à lire
`internal/ics/` (créé en T015), `internal/auth/`.

## Spécification
- Token d'abonnement par utilisateur (table `calendar_feed_token`, hash stocké), régénérable depuis `/admin/settings`.
- `GET /ics/{token}.ics` : RDV de J-30 à J+90, `UID` stable = id du RDV, `SEQUENCE` = version, RDV annulés en `STATUS:CANCELLED`.
- **Minimisation** : par défaut le titre = « RDV » + initiales + motif ; nom complet seulement si l'option est cochée (avertissement : le flux transite par Google/Apple).
- `ETag` + `Cache-Control: private, max-age=300`.

## Critères d'acceptation
- [ ] Fichier validé (test golden + import manuel dans un client, noter lequel dans la PR).
- [ ] Token révoqué → 404.

## Vérification
```bash
make check
```

## Hors scope
Synchronisation bidirectionnelle / CalDAV.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
