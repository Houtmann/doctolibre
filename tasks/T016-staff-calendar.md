# T016 — Agenda praticien

- **Phase** : 1
- **Dépend de** : T012, T013
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le praticien voit et gère son agenda jour/semaine : RDV, absences, création manuelle (patient au téléphone), déplacement, annulation, statut honoré/absent.

## Contexte à lire
`internal/service/booking.go` (si existe, sinon `internal/booking`), layout admin, tables `appointment`, `patient`, `availability_*`.

## Spécification
- `GET /admin/calendar?view=day|week&date=YYYY-MM-DD` : grille CSS (`display:grid`, lignes de 15 min, 07:00–21:00 configurable), RDV positionnés en absolu par calcul serveur (top/height en %). Zones hors dispo grisées. Navigation précédent/suivant/aujourd'hui via htmx (swap de la grille seule).
- Vue mobile : liste chronologique du jour au lieu de la grille (< 640 px, via CSS + template alternatif).
- Clic sur créneau vide → `GET /admin/appointments/new?start=` (modale `<dialog>`) : recherche patient (htmx, 300 ms debounce) ou création rapide, motif, praticien.
  Le personnel **peut** forcer un RDV hors dispo (checkbox « hors horaires ») mais **jamais** en chevauchement de zone bloquée (`during_blocked`, buffers inclus — cf. T010). Création et déplacement passent par la même fonction du store que T014.
- Clic sur RDV → détail : patient, motif, source, historique ; actions : déplacer (choisir nouvelle date/heure), annuler (motif), marquer `done` / `no_show`.
- Indicateur « maintenant » (ligne rouge) dans la vue du jour.
- Rafraîchissement : `hx-trigger="every 60s"` sur la grille (pas de websocket).

## Critères d'acceptation
- [ ] Afficher une semaine avec 150 RDV en < 100 ms côté serveur (log de durée).
- [ ] Toutes les actions passent par `service` et émettent un `EventSink`.
- [ ] Tests handler pour création / déplacement / annulation / conflit.
- [ ] Utilisable au clavier (tab vers chaque RDV, Entrée ouvre le détail).

## Vérification
```bash
make check
```

## Hors scope
Drag & drop (plus tard, éventuellement), multi-praticiens côte à côte (T033), séries (T030).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
