package booking

import (
	"sort"
	"time"
)

// Interval est un intervalle semi-ouvert [Start, End) d'instants absolus.
type Interval struct {
	Start, End time.Time
}

func (i Interval) empty() bool { return !i.End.After(i.Start) }

// Overlaps indique si deux intervalles [) se chevauchent. L'adjacence
// (a.End == b.Start) n'est PAS un chevauchement : même règle que l'opérateur
// && de tstzrange utilisé par la contrainte EXCLUDE.
func (i Interval) Overlaps(o Interval) bool {
	return i.Start.Before(o.End) && o.Start.Before(i.End)
}

// Union renvoie les intervalles fusionnés, triés, disjoints et non adjacents.
// Les intervalles vides ou inversés sont ignorés. L'entrée n'est pas modifiée.
func Union(in []Interval) []Interval {
	tmp := make([]Interval, 0, len(in))
	for _, i := range in {
		if !i.empty() {
			tmp = append(tmp, i)
		}
	}
	sort.Slice(tmp, func(a, b int) bool { return tmp[a].Start.Before(tmp[b].Start) })
	out := tmp[:0]
	for _, i := range tmp {
		if n := len(out); n > 0 && !i.Start.After(out[n-1].End) {
			if i.End.After(out[n-1].End) {
				out[n-1].End = i.End
			}
			continue
		}
		out = append(out, i)
	}
	return out
}

// Subtract renvoie a privé de b.
func Subtract(a, b []Interval) []Interval {
	a, b = Union(a), Union(b)
	var out []Interval
	j := 0
	for _, x := range a {
		cur := x.Start
		for j < len(b) && !b[j].End.After(cur) {
			j++
		}
		for k := j; k < len(b) && b[k].Start.Before(x.End); k++ {
			if b[k].Start.After(cur) {
				out = append(out, Interval{cur, b[k].Start})
			}
			if b[k].End.After(cur) {
				cur = b[k].End
			}
		}
		if cur.Before(x.End) {
			out = append(out, Interval{cur, x.End})
		}
	}
	return out
}

// Intersect renvoie l'intersection de a et b.
func Intersect(a, b []Interval) []Interval {
	a, b = Union(a), Union(b)
	var out []Interval
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		s := a[i].Start
		if b[j].Start.After(s) {
			s = b[j].Start
		}
		e := a[i].End
		if b[j].End.Before(e) {
			e = b[j].End
		}
		if s.Before(e) {
			out = append(out, Interval{s, e})
		}
		if a[i].End.Before(b[j].End) {
			i++
		} else {
			j++
		}
	}
	return out
}
