package booking

import (
	"sort"
	"time"
)

// SeriesRequest décrit une série de séances à proposer (algorithme glouton).
type SeriesRequest struct {
	Loc      *time.Location
	Count    int            // nombre de séances à placer
	PerWeek  int            // séances par semaine (lundi-dimanche) ; < 1 = 1
	Weekdays []time.Weekday // jours préférés ; vide = tous
	// Fenêtre horaire souhaitée (heure murale) : un créneau est retenu si le
	// soin tient dans [WindowStart, WindowEnd]. Les deux à 0 = pas de contrainte.
	WindowStart, WindowEnd LocalTime
	From                   Date // premier jour éligible (inclus)
	// MinGapDays : écart minimal en jours civils entre deux séances (>= 1 ;
	// 2 = au moins un jour sans séance entre deux).
	MinGapDays int
	// Taken : dates des séances déjà planifiées de la série (remplacement) :
	// elles comptent pour le quota hebdomadaire et pour l'écart minimal.
	Taken []Date
}

// ProposeSeries choisit, parmi des créneaux triés (sortie de Slots), jusqu'à
// Count séances : semaine par semaine, d'abord les jours préférés puis, s'il
// en manque, les autres jours ; un seul créneau (le premier compatible) par
// jour, en respectant l'écart minimal. Le résultat est trié et peut contenir
// moins de Count séances si les créneaux manquent. Fonction pure.
func ProposeSeries(slots []Slot, req SeriesRequest) []Slot {
	if req.Count <= 0 || req.Loc == nil {
		return nil
	}
	perWeek := max(req.PerWeek, 1)
	gap := max(req.MinGapDays, 1)
	from := dayNumber(req.From)

	pref := map[time.Weekday]bool{}
	for _, w := range req.Weekdays {
		pref[w] = true
	}

	type cand struct {
		day  int
		slot Slot
		pref bool
	}
	byWeek := map[int][]cand{}
	seenDay := map[int]bool{}
	var weeks []int
	for _, s := range slots {
		ls := s.Start.In(req.Loc)
		day := dayNumber(DateOf(ls))
		if day < from || seenDay[day] || !req.inWindow(s, ls) {
			continue
		}
		seenDay[day] = true
		w := weekNumber(day)
		if len(byWeek[w]) == 0 {
			weeks = append(weeks, w)
		}
		byWeek[w] = append(byWeek[w], cand{day, s, len(pref) == 0 || pref[ls.Weekday()]})
	}
	sort.Ints(weeks)

	var chosenDays []int // séances déjà retenues + Taken, pour l'écart minimal
	takenInWeek := map[int]int{}
	for _, d := range req.Taken {
		n := dayNumber(d)
		chosenDays = append(chosenDays, n)
		takenInWeek[weekNumber(n)]++
	}
	okGap := func(day int) bool {
		for _, c := range chosenDays {
			if abs(day-c) < gap {
				return false
			}
		}
		return true
	}

	var out []Slot
	for _, w := range weeks {
		n := takenInWeek[w]
		for _, wantPref := range []bool{true, false} {
			for _, c := range byWeek[w] { // chronologique : slots triés
				if len(out) >= req.Count || n >= perWeek {
					break
				}
				if c.pref != wantPref || !okGap(c.day) {
					continue
				}
				out = append(out, c.slot)
				chosenDays = append(chosenDays, c.day)
				n++
			}
		}
		if len(out) >= req.Count {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

func (r SeriesRequest) inWindow(s Slot, local time.Time) bool {
	if r.WindowStart == 0 && r.WindowEnd == 0 {
		return true
	}
	start := local.Hour()*60 + local.Minute()
	end := start + int(s.End.Sub(s.Start)/time.Minute)
	return start >= int(r.WindowStart) && end <= int(r.WindowEnd)
}

// dayNumber numérote les jours civils (indépendant du fuseau et du DST).
func dayNumber(d Date) int {
	return int(time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC).Unix() / 86400)
}

// weekNumber numérote les semaines lundi-dimanche (le jour 0, 1970-01-01, est un jeudi).
func weekNumber(day int) int { return (day + 3) / 7 }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
