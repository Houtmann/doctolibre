package booking

import (
	"reflect"
	"testing"
	"time"
)

func iv(a, b int) Interval {
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	return Interval{t0.Add(time.Duration(a) * time.Minute), t0.Add(time.Duration(b) * time.Minute)}
}

func TestIntervalOps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  []Interval
		want []Interval
	}{
		{"union vide", Union(nil), nil},
		{"union disjoints triés", Union([]Interval{iv(20, 30), iv(0, 10)}), []Interval{iv(0, 10), iv(20, 30)}},
		{"union chevauchement", Union([]Interval{iv(0, 10), iv(5, 15)}), []Interval{iv(0, 15)}},
		{"union adjacents fusionnés", Union([]Interval{iv(0, 10), iv(10, 20)}), []Interval{iv(0, 20)}},
		{"union inclus", Union([]Interval{iv(0, 30), iv(5, 10)}), []Interval{iv(0, 30)}},
		{"union ignore vides", Union([]Interval{iv(5, 5), iv(9, 3)}), []Interval{}},
		{"subtract milieu", Subtract([]Interval{iv(0, 30)}, []Interval{iv(10, 20)}), []Interval{iv(0, 10), iv(20, 30)}},
		{"subtract tout", Subtract([]Interval{iv(0, 30)}, []Interval{iv(-5, 40)}), nil},
		{"subtract bord gauche", Subtract([]Interval{iv(0, 30)}, []Interval{iv(0, 10)}), []Interval{iv(10, 30)}},
		{"subtract disjoint", Subtract([]Interval{iv(0, 10)}, []Interval{iv(10, 20)}), []Interval{iv(0, 10)}},
		{"subtract plusieurs", Subtract([]Interval{iv(0, 100)}, []Interval{iv(10, 20), iv(30, 40), iv(90, 120)}),
			[]Interval{iv(0, 10), iv(20, 30), iv(40, 90)}},
		{"subtract b couvre plusieurs a", Subtract([]Interval{iv(0, 10), iv(20, 30), iv(40, 50)}, []Interval{iv(5, 45)}),
			[]Interval{iv(0, 5), iv(45, 50)}},
		{"subtract rien", Subtract([]Interval{iv(0, 10)}, nil), []Interval{iv(0, 10)}},
		{"intersect", Intersect([]Interval{iv(0, 20), iv(30, 50)}, []Interval{iv(10, 40)}), []Interval{iv(10, 20), iv(30, 40)}},
		{"intersect adjacents", Intersect([]Interval{iv(0, 10)}, []Interval{iv(10, 20)}), nil},
		{"intersect vide", Intersect(nil, []Interval{iv(0, 10)}), nil},
	}
	for _, tc := range tests {
		if len(tc.got) == 0 && len(tc.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestOverlaps(t *testing.T) {
	t.Parallel()
	if iv(0, 10).Overlaps(iv(10, 20)) || iv(10, 20).Overlaps(iv(0, 10)) {
		t.Error("l'adjacence ne doit pas chevaucher")
	}
	if !iv(0, 11).Overlaps(iv(10, 20)) || !iv(0, 100).Overlaps(iv(10, 20)) {
		t.Error("chevauchement attendu")
	}
}

func TestParseLocalTime(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]LocalTime{"08:00": 480, "00:00": 0, "24:00": 1440, "17:45": 1065} {
		got, err := ParseLocalTime(in)
		if err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, in := range []string{"", "25:00", "12:60", "x", "-1:00"} {
		if _, err := ParseLocalTime(in); err == nil {
			t.Errorf("%q devrait échouer", in)
		}
	}
}
