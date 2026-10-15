# T013 — Moteur de calcul de créneaux

- **Phase** : 1
- **Dépend de** : T001
- **Taille estimée** : M — **tâche critique, soigner les tests**
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
Une fonction **pure** (sans DB, sans horloge globale) qui calcule les créneaux réservables d'un praticien sur une période.

## Contexte à lire
`PLAN.md` §3 et §4, `AGENTS.md` (section Temps).

## Spécification
Package `internal/booking` (aucun import de `store`, `web`, `service`).

```go
type WeeklyRule struct {
    Weekday   time.Weekday
    Start     LocalTime // heure murale "08:00"
    End       LocalTime
    ValidFrom civil.Date // ou type Date maison
    ValidUntil *Date
    TypeIDs   []uuid.UUID // vide = tous
}
type Exception struct { Start, End time.Time; Kind ExceptionKind /* Closed | Open */ }
type Busy struct { Start, End time.Time } // = during_blocked des RDV existants non annulés
type Request struct {
    Loc         *time.Location
    From, To    time.Time
    Now         time.Time
    MinNotice   time.Duration
    Horizon     time.Duration
    Duration    time.Duration
    BufferBefore, BufferAfter time.Duration
    Step        time.Duration // granularité de départ, défaut = 15 min ou Duration si plus petite
    TypeID      uuid.UUID
}
func Slots(req Request, rules []WeeklyRule, exc []Exception, busy []Busy) []Slot // Slot{Start, End}
```

Algorithme :
1. Pour chaque jour local entre From et To (dans `Loc`), matérialiser les plages des règles valides ce jour-là et compatibles avec `TypeID` → intervalles absolus (attention DST : construire avec `time.Date(y,m,d,h,mi,0,0,loc)`).
2. Ajouter les exceptions `Open`, soustraire les exceptions `Closed`.
3. Un créneau candidat `[s, s+Duration)` est valide si sa zone bloquée `[s-BufferBefore, s+Duration+BufferAfter)` ne chevauche aucun `Busy` ni exception `Closed`. C'est exactement la règle de la contrainte `EXCLUDE` de T010 : le moteur et la base doivent toujours être d'accord.
   Le soin lui-même (`[s, s+Duration)`) doit tenir dans une plage ouverte ; les buffers peuvent déborder de la plage (choix à confirmer et noter dans `docs/decisions.md`).
4. Découper chaque intervalle libre en créneaux de `Duration` tous les `Step`, alignés sur le début de plage.
5. Filtrer `Start < Now + MinNotice` et `Start > Now + Horizon`.
6. Résultat trié, sans doublon.

Utilitaires internes : `type Interval`, `Union`, `Subtract`, `Intersect` — testés séparément.

## Critères d'acceptation
- [x] Tests table-driven couvrant : journée simple ; pause midi ; absence partielle ; absence journée ; ouverture exceptionnelle un dimanche ; RDV existant au milieu ; buffers ; notice minimale ; horizon ; motif non autorisé sur une plage ; validité de règle (valid_from/until).
- [x] **DST** : dimanche 30/03/2025 et 26/10/2025 en `Europe/Paris` — pas de créneau fantôme ni manquant, durées réelles respectées.
- [x] Fuzz test (`testing.F`) : la zone bloquée d'aucun créneau ne chevauche un `Busy` ni une exception `Closed` ; tous les soins sont dans une règle ou une exception `Open`.
- [x] Benchmark : 1 praticien, 60 jours, 500 RDV → < 5 ms.

## Vérification
```bash
go test ./internal/booking/... -run . -fuzz FuzzSlots -fuzztime 30s && go test -bench . ./internal/booking/
```

## Hors scope
Chargement depuis la DB (fait dans `service`, T014/T016).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : `internal/domain/blocked.go` (`BlockedRange`, partagée avec T010/T014/T016) ; `internal/booking/{interval,slots}.go` (Interval/Union/Subtract/Intersect, `Slots`, LocalTime, Date) ; tests table-driven, DST (30/03 et 26/10/2025), fuzz `FuzzSlots` (30 s, ~955k exécutions OK), benchmark 60 j / 500 RDV ≈ 0,26 ms. `make check` vert.
- **Reste à faire** : rien.
- **Notes de reprise** : `go get github.com/google/uuid` a ajouté la dépendance à go.mod (autorisée) : possible conflit go.mod/go.sum à fusionner avec T002/T003.
- **Écarts avec la spécification** : `ValidFrom/ValidUntil` sont `*Date` (type maison, pas civil.Date). Les exceptions `Closed` retirent aussi la dispo ET bloquent tout créneau dont la zone bloquée (buffers inclus) les touche. `Horizon <= 0` = pas de limite ; `Start == Now+Horizon` accepté. `[From,To)` filtre sur le début du créneau. Buffers autorisés à déborder de la plage ouverte (choix de la spec, à noter dans decisions.md). Exception `Open` valable pour tous les motifs.
