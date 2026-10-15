package booking

import (
	"slices"
	"testing"
	"time"
)

// openWeek : lundi-vendredi 09:00-12:00 (Paris), créneaux de 30 min.
func seriesSlots(t *testing.T, from, to time.Time, busy []Busy) []Slot {
	t.Helper()
	var rules []WeeklyRule
	for wd := time.Monday; wd <= time.Friday; wd++ {
		rules = append(rules, WeeklyRule{Weekday: wd, Start: lt(9, 0), End: lt(12, 0)})
	}
	return Slots(Request{Loc: paris, From: from, To: to, Now: from.AddDate(0, 0, -7), Duration: 30 * time.Minute, Step: 30 * time.Minute}, rules, nil, busy)
}

func TestProposeSeries(t *testing.T) {
	t.Parallel()
	from, to := at(2026, 10, 19, 0, 0), at(2026, 12, 20, 0, 0)
	mondayThursday := []time.Weekday{time.Monday, time.Thursday}

	tests := []struct {
		name string
		busy []Busy
		req  SeriesRequest
		want []string
	}{
		{
			name: "10 séances, 2 par semaine, lundi et jeudi, à travers le passage à l'heure d'hiver",
			req:  SeriesRequest{Count: 10, PerWeek: 2, Weekdays: mondayThursday, From: Date{2026, 10, 19}, MinGapDays: 2},
			want: []string{"10-19 09:00", "10-22 09:00", "10-26 09:00", "10-29 09:00", "11-02 09:00", "11-05 09:00", "11-09 09:00", "11-12 09:00", "11-16 09:00", "11-19 09:00"},
		},
		{
			name: "sans jour préféré : écart minimal d'un jour sans séance",
			req:  SeriesRequest{Count: 4, PerWeek: 2, From: Date{2026, 10, 19}, MinGapDays: 2},
			want: []string{"10-19 09:00", "10-21 09:00", "10-26 09:00", "10-28 09:00"},
		},
		{
			name: "jour préféré occupé : repli sur un autre jour de la semaine",
			busy: []Busy{{at(2026, 10, 26, 8, 0), at(2026, 10, 26, 13, 0)}},
			req:  SeriesRequest{Count: 4, PerWeek: 2, Weekdays: mondayThursday, From: Date{2026, 10, 19}, MinGapDays: 2},
			want: []string{"10-19 09:00", "10-22 09:00", "10-27 09:00", "10-29 09:00"},
		},
		{
			name: "début en milieu de semaine : le premier jour éligible compte",
			req:  SeriesRequest{Count: 3, PerWeek: 2, Weekdays: mondayThursday, From: Date{2026, 10, 21}, MinGapDays: 2},
			want: []string{"10-22 09:00", "10-26 09:00", "10-29 09:00"},
		},
		{
			name: "fenêtre horaire : 10h30 au plus tôt",
			req:  SeriesRequest{Count: 2, PerWeek: 1, From: Date{2026, 10, 19}, WindowStart: lt(10, 30), WindowEnd: lt(12, 0), MinGapDays: 1},
			want: []string{"10-19 10:30", "10-26 10:30"},
		},
		{
			name: "fenêtre sans créneau : aucune proposition",
			req:  SeriesRequest{Count: 2, PerWeek: 1, From: Date{2026, 10, 19}, WindowStart: lt(14, 0), WindowEnd: lt(16, 0), MinGapDays: 1},
			want: []string{},
		},
		{
			name: "remplacement : la semaine pleine est sautée",
			req: SeriesRequest{Count: 1, PerWeek: 2, Weekdays: mondayThursday, From: Date{2026, 10, 19}, MinGapDays: 2,
				Taken: []Date{{2026, 10, 19}, {2026, 10, 22}}},
			want: []string{"10-26 09:00"},
		},
		{
			name: "remplacement : une séance manque cette semaine, l'écart est respecté",
			req: SeriesRequest{Count: 1, PerWeek: 2, From: Date{2026, 10, 19}, MinGapDays: 2,
				Taken: []Date{{2026, 10, 19}}},
			want: []string{"10-21 09:00"},
		},
		{
			name: "moins de créneaux que demandé : proposition partielle",
			req:  SeriesRequest{Count: 10, PerWeek: 1, From: Date{2026, 12, 14}, MinGapDays: 1},
			want: []string{"12-14 09:00"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.req.Loc = paris
			slots := seriesSlots(t, from, to, tt.busy)
			if tt.name == "moins de créneaux que demandé : proposition partielle" {
				slots = seriesSlots(t, at(2026, 12, 14, 0, 0), at(2026, 12, 15, 0, 0), nil)
			}
			got := fmtSlots(ProposeSeries(slots, tt.req))
			if !slices.Equal(got, tt.want) {
				t.Errorf("got  %v\nwant %v", got, tt.want)
			}
		})
	}
}

// Les séances gardent la même heure murale de part et d'autre du changement
// d'heure (25 octobre 2026) : seul l'instant UTC change.
func TestProposeSeriesKeepsWallClockAcrossDST(t *testing.T) {
	t.Parallel()
	slots := seriesSlots(t, at(2026, 10, 19, 0, 0), at(2026, 11, 10, 0, 0), nil)
	got := ProposeSeries(slots, SeriesRequest{Loc: paris, Count: 4, PerWeek: 2, Weekdays: []time.Weekday{time.Monday, time.Thursday}, From: Date{2026, 10, 19}, MinGapDays: 2})
	if len(got) != 4 {
		t.Fatalf("len = %d", len(got))
	}
	for i, s := range got {
		if h := s.Start.In(paris).Hour(); h != 9 {
			t.Errorf("séance %d à %dh", i+1, h)
		}
	}
	if before, after := got[1].Start.UTC().Hour(), got[2].Start.UTC().Hour(); before != 7 || after != 8 {
		t.Errorf("UTC avant/après le changement d'heure = %d/%d, attendu 7/8", before, after)
	}
}
