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
- [x] Fichier validé par test golden (`internal/ics/testdata/feed.ics`) — **import manuel dans un client non fait** (à faire par l'humain, noter lequel dans la PR).
- [x] Token révoqué → 404.

## Vérification
```bash
make check
```

## Hors scope
Synchronisation bidirectionnelle / CalDAV.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : migration `00011_calendar_feed_token` (1 ligne/utilisateur : sha256 du jeton, `full_names`) ; requêtes `calendar_feed.sql` + sqlc ; `ics.Feed` (+ `Status`, `Sequence` sur `Event`, `X-WR-CALNAME`) avec golden `testdata/feed.ics` (DST) ; `service.CalendarFeed` (Status/Regenerate/SetFullNames/Revoke/Render, fenêtre J-30..J+90, ETag = sha256 du corps, titre « RDV L.D. - Motif » ou nom complet si option) ; `GET /ics/{token}.ics` (404/304/ETag/`Cache-Control: private, max-age=300`) ; panneau « Abonnement à l'agenda » dans `/admin/settings` (activer/régénérer, option nom complet, révoquer ; lien affiché une seule fois, `no-store`) ; tests service + web ; `httpx.Logger` masque le jeton des chemins `/ics/` et `/r/`.
- **Reste à faire** : import manuel dans un client calendrier (Apple/Google/Thunderbird) ; vérification visuelle du panneau réglages.
- **Notes de reprise** : EXPLAIN (200 000 RDV, 5 praticiens, fenêtre 120 j) : Bitmap Index Scan sur `appointment_practitioner_blocked_idx`, ~4 ms. Pas d'index ajouté.
- **Écarts avec la spécification** : (1) pas de colonne « version » sur `appointment` → `SEQUENCE` = `updated_at` en secondes Unix (monotone, change à chaque modification) ; `DTSTAMP` = `updated_at` (ETag stable). (2) « comparaison en temps constant » : la recherche est par égalité du sha256 sur un index unique (jeton 256 bits), pas de comparaison applicative. (3) RDV `done`/`no_show` sont `CONFIRMED`. (4) Ajout hors liste : masquage du jeton dans `httpx.Logger`.
