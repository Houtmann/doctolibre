// Package booking calcule les créneaux réservables. Il est pur : aucune DB,
// aucune horloge globale (l'instant courant est passé dans Request.Now).
package booking

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/domain"
)

// LocalTime est une heure murale, en minutes depuis minuit (0..1440).
type LocalTime int

// NewLocalTime construit une heure murale ; 24:00 est accepté (fin de journée).
func NewLocalTime(h, m int) LocalTime { return LocalTime(h*60 + m) }

// ParseLocalTime lit "HH:MM".
func ParseLocalTime(s string) (LocalTime, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || h < 0 || m < 0 || m > 59 || h*60+m > 1440 {
		return 0, fmt.Errorf("heure invalide %q", s)
	}
	return NewLocalTime(h, m), nil
}

// Date est une date civile (sans fuseau).
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf renvoie la date civile de t dans son propre fuseau.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

func (d Date) before(o Date) bool {
	if d.Year != o.Year {
		return d.Year < o.Year
	}
	if d.Month != o.Month {
		return d.Month < o.Month
	}
	return d.Day < o.Day
}

// WeeklyRule est une plage hebdomadaire récurrente en heure murale.
type WeeklyRule struct {
	Weekday    time.Weekday
	Start, End LocalTime
	ValidFrom  *Date       // inclusif ; nil = sans borne
	ValidUntil *Date       // inclusif ; nil = sans borne
	TypeIDs    []uuid.UUID // vide = tous les motifs
}

// ExceptionKind distingue absence (Closed) et ouverture exceptionnelle (Open).
type ExceptionKind int

const (
	// Closed : absence. Retire de la disponibilité ET bloque les créneaux
	// dont la zone bloquée la touche.
	Closed ExceptionKind = iota
	// Open : ouverture exceptionnelle (tous motifs).
	Open
)

// Exception est une absence ou une ouverture ponctuelle (instants absolus).
type Exception struct {
	Start, End time.Time
	Kind       ExceptionKind
}

// Busy est la zone bloquée (during_blocked) d'un RDV non annulé.
type Busy struct{ Start, End time.Time }

// Request décrit le calcul demandé (tous les instants sont injectés, jamais lus).
type Request struct {
	Loc          *time.Location
	From, To     time.Time // créneaux dont le début est dans [From, To)
	Now          time.Time
	MinNotice    time.Duration
	Horizon      time.Duration // <= 0 : pas de limite
	Duration     time.Duration
	BufferBefore time.Duration
	BufferAfter  time.Duration
	Step         time.Duration // <= 0 : min(15 min, Duration)
	TypeID       uuid.UUID
	// ResourceBusy : occupations (during_blocked) de la ressource exigée par le motif
	// (T060), tous praticiens confondus. Même règle que Busy ; vide = motif sans ressource.
	ResourceBusy []Busy
}

// Slot est un créneau réservable [Start, End) pour le soin seul (hors buffers).
type Slot struct{ Start, End time.Time }

// Slots calcule les créneaux réservables, triés et sans doublon.
//
// Règle (identique à la contrainte EXCLUDE de la base) : un créneau [s, e) est
// proposé si le soin tient dans une plage ouverte et si sa zone bloquée
// [s-avant, e+après) ne chevauche (intervalles [) ) aucun Busy ni aucune
// exception Closed. Les buffers peuvent déborder de la plage ouverte.
func Slots(req Request, rules []WeeklyRule, exc []Exception, busy []Busy) []Slot {
	if req.Duration <= 0 || req.Loc == nil || !req.From.Before(req.To) {
		return nil
	}
	step := req.Step
	if step <= 0 {
		step = 15 * time.Minute
		if req.Duration < step {
			step = req.Duration
		}
	}

	var open, closed []Interval
	// 1. plages hebdo par jour local. AddDate (jamais +24h) pour passer au jour suivant.
	first, last := DateOf(req.From.In(req.Loc)), DateOf(req.To.In(req.Loc))
	day := time.Date(first.Year, first.Month, first.Day, 12, 0, 0, 0, req.Loc)
	for d := first; !last.before(d); {
		for _, r := range rules {
			if r.Weekday != day.Weekday() || !r.valid(d) || !r.allows(req.TypeID) {
				continue
			}
			s := time.Date(d.Year, d.Month, d.Day, 0, int(r.Start), 0, 0, req.Loc)
			e := time.Date(d.Year, d.Month, d.Day, 0, int(r.End), 0, 0, req.Loc)
			open = append(open, Interval{s, e})
		}
		day = day.AddDate(0, 0, 1)
		d = DateOf(day)
	}
	// 2. exceptions : Open ajoute, Closed soustrait.
	for _, x := range exc {
		if x.Kind == Open {
			open = append(open, Interval{x.Start, x.End})
		} else {
			closed = append(closed, Interval{x.Start, x.End})
		}
	}
	closed = Union(closed)
	avail := Subtract(open, closed)

	blocked := make([]Interval, 0, len(busy)+len(req.ResourceBusy)+len(closed))
	blocked = append(blocked, closed...)
	for _, b := range busy {
		blocked = append(blocked, Interval(b))
	}
	for _, b := range req.ResourceBusy {
		blocked = append(blocked, Interval(b))
	}
	blocked = Union(blocked)

	minStart := req.Now.Add(req.MinNotice)
	var out []Slot
	for _, iv := range avail { // disjoints et triés : pas de doublon possible
		for s := iv.Start; ; s = s.Add(step) {
			e := s.Add(req.Duration)
			if e.After(iv.End) {
				break
			}
			if !s.Before(req.To) || (req.Horizon > 0 && s.After(req.Now.Add(req.Horizon))) {
				break
			}
			if s.Before(req.From) || s.Before(minStart) {
				continue
			}
			bs, be := domain.BlockedRange(s, e, req.BufferBefore, req.BufferAfter)
			if !overlapsAny(blocked, Interval{bs, be}) {
				out = append(out, Slot{s, e})
			}
		}
	}
	return out
}

func (r WeeklyRule) valid(d Date) bool {
	if r.ValidFrom != nil && d.before(*r.ValidFrom) {
		return false
	}
	if r.ValidUntil != nil && r.ValidUntil.before(d) {
		return false
	}
	return true
}

func (r WeeklyRule) allows(id uuid.UUID) bool {
	if len(r.TypeIDs) == 0 {
		return true
	}
	for _, t := range r.TypeIDs {
		if t == id {
			return true
		}
	}
	return false
}

// overlapsAny : blocked est trié et disjoint (sortie de Union) → recherche binaire.
func overlapsAny(blocked []Interval, z Interval) bool {
	i := sort.Search(len(blocked), func(i int) bool { return blocked[i].End.After(z.Start) })
	return i < len(blocked) && blocked[i].Start.Before(z.End)
}
