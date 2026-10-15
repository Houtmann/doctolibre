package booking

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
)

var paris = mustLoc("Europe/Paris")

func mustLoc(n string) *time.Location {
	l, err := time.LoadLocation(n)
	if err != nil {
		panic(err)
	}
	return l
}

func at(y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, paris)
}

func lt(h, m int) LocalTime { return NewLocalTime(h, m) }

// fmtSlots rend "HH:MM" locaux (Paris) pour comparer lisiblement.
func fmtSlots(ss []Slot) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Start.In(paris).Format("01-02 15:04")
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hours(day string, from, to int) []string {
	var out []string
	for h := from; h < to; h++ {
		out = append(out, fmt.Sprintf("%s %02d:00", day, h))
	}
	return out
}

// Lundi 2 juin 2025, hors DST.
func baseReq() Request {
	return Request{
		Loc:      paris,
		From:     at(2025, 6, 2, 0, 0),
		To:       at(2025, 6, 3, 0, 0),
		Now:      at(2025, 1, 1, 0, 0),
		Duration: time.Hour,
		Step:     time.Hour,
	}
}

func monRule(sh, eh int) WeeklyRule {
	return WeeklyRule{Weekday: time.Monday, Start: lt(sh, 0), End: lt(eh, 0)}
}

func TestSlots(t *testing.T) {
	t.Parallel()
	typeA, typeB := uuid.New(), uuid.New()
	d := func(y int, m time.Month, dd int) *Date { return &Date{y, m, dd} }

	tests := []struct {
		name  string
		mod   func(*Request)
		rules []WeeklyRule
		exc   []Exception
		busy  []Busy
		want  []string
	}{
		{name: "journée simple", rules: []WeeklyRule{monRule(9, 12)},
			want: hours("06-02", 9, 12)},
		{name: "pause midi", rules: []WeeklyRule{monRule(9, 12), monRule(14, 16)},
			want: append(hours("06-02", 9, 12), hours("06-02", 14, 16)...)},
		{name: "absence partielle", rules: []WeeklyRule{monRule(9, 17)},
			exc:  []Exception{{at(2025, 6, 2, 11, 0), at(2025, 6, 2, 13, 0), Closed}},
			want: append(hours("06-02", 9, 11), hours("06-02", 13, 17)...)},
		{name: "absence journée", rules: []WeeklyRule{monRule(9, 17)},
			exc: []Exception{{at(2025, 6, 2, 0, 0), at(2025, 6, 3, 0, 0), Closed}}, want: nil},
		{name: "ouverture exceptionnelle un dimanche",
			mod:   func(r *Request) { r.From, r.To = at(2025, 6, 1, 0, 0), at(2025, 6, 2, 0, 0) },
			rules: []WeeklyRule{monRule(9, 12)},
			exc:   []Exception{{at(2025, 6, 1, 10, 0), at(2025, 6, 1, 12, 0), Open}},
			want:  hours("06-01", 10, 12)},
		{name: "RDV existant au milieu", rules: []WeeklyRule{monRule(9, 13)},
			busy: []Busy{{at(2025, 6, 2, 10, 0), at(2025, 6, 2, 11, 0)}},
			want: []string{"06-02 09:00", "06-02 11:00", "06-02 12:00"}},
		{name: "RDV chevauchant partiellement (pas aligné)", rules: []WeeklyRule{monRule(9, 13)},
			busy: []Busy{{at(2025, 6, 2, 10, 30), at(2025, 6, 2, 11, 30)}},
			want: []string{"06-02 09:00", "06-02 12:00"}},
		{name: "adjacence = pas de chevauchement", rules: []WeeklyRule{monRule(9, 11)},
			busy: []Busy{{at(2025, 6, 2, 10, 0), at(2025, 6, 2, 12, 0)}},
			want: []string{"06-02 09:00"}},
		{name: "buffers vs busy",
			mod: func(r *Request) {
				r.BufferBefore, r.BufferAfter = 15*time.Minute, 15*time.Minute
				r.Step = 15 * time.Minute
			},
			rules: []WeeklyRule{monRule(9, 13)},
			busy:  []Busy{{at(2025, 6, 2, 10, 0), at(2025, 6, 2, 11, 0)}},
			// zone [s-15, s+75) doit être hors [10:00,11:00) : s+75 <= 10:00 -> s <= 08:45 (n/a) ; s-15 >= 11:00 -> s >= 11:15
			// 12:00 inclus : le soin finit à 13:00 (bord de plage), le buffer après déborde (permis).
			want: []string{"06-02 11:15", "06-02 11:30", "06-02 11:45", "06-02 12:00"}},
		{name: "buffers peuvent déborder de la plage",
			mod:   func(r *Request) { r.BufferBefore, r.BufferAfter = 30*time.Minute, 30*time.Minute },
			rules: []WeeklyRule{monRule(9, 10)},
			want:  []string{"06-02 09:00"}},
		{name: "buffer adjacent au busy accepté",
			mod:   func(r *Request) { r.BufferAfter = 30 * time.Minute },
			rules: []WeeklyRule{monRule(9, 12)},
			busy:  []Busy{{at(2025, 6, 2, 10, 30), at(2025, 6, 2, 11, 0)}},
			want:  []string{"06-02 09:00", "06-02 11:00"}},
		{name: "buffer touchant une absence",
			mod:   func(r *Request) { r.BufferAfter = 30 * time.Minute },
			rules: []WeeklyRule{monRule(9, 12)},
			exc:   []Exception{{at(2025, 6, 2, 10, 30), at(2025, 6, 2, 13, 0), Closed}},
			want:  []string{"06-02 09:00"}},
		{name: "notice minimale",
			mod: func(r *Request) {
				r.Now = at(2025, 6, 2, 8, 0)
				r.MinNotice = 90 * time.Minute // 09:30
			},
			rules: []WeeklyRule{monRule(9, 13)}, want: hours("06-02", 10, 13)},
		{name: "horizon",
			mod: func(r *Request) {
				r.Now = at(2025, 6, 2, 8, 0)
				r.Horizon = 2 * time.Hour // 10:00 inclus
			},
			rules: []WeeklyRule{monRule(9, 13)}, want: hours("06-02", 9, 11)},
		{name: "motif non autorisé",
			mod:   func(r *Request) { r.TypeID = typeB },
			rules: []WeeklyRule{{Weekday: time.Monday, Start: lt(9, 0), End: lt(12, 0), TypeIDs: []uuid.UUID{typeA}}, monRule(14, 15)},
			want:  []string{"06-02 14:00"}},
		{name: "motif autorisé",
			mod:   func(r *Request) { r.TypeID = typeA },
			rules: []WeeklyRule{{Weekday: time.Monday, Start: lt(9, 0), End: lt(10, 0), TypeIDs: []uuid.UUID{typeA}}},
			want:  []string{"06-02 09:00"}},
		{name: "valid_from futur", rules: []WeeklyRule{{Weekday: time.Monday, Start: lt(9, 0), End: lt(10, 0), ValidFrom: d(2025, 6, 3)}}, want: nil},
		{name: "valid_from inclusif", rules: []WeeklyRule{{Weekday: time.Monday, Start: lt(9, 0), End: lt(10, 0), ValidFrom: d(2025, 6, 2)}}, want: []string{"06-02 09:00"}},
		{name: "valid_until passé", rules: []WeeklyRule{{Weekday: time.Monday, Start: lt(9, 0), End: lt(10, 0), ValidUntil: d(2025, 6, 1)}}, want: nil},
		{name: "valid_until inclusif", rules: []WeeklyRule{{Weekday: time.Monday, Start: lt(9, 0), End: lt(10, 0), ValidUntil: d(2025, 6, 2)}}, want: []string{"06-02 09:00"}},
		{name: "step 30 min, durée 45, alignés sur le début de plage",
			mod:   func(r *Request) { r.Duration, r.Step = 45*time.Minute, 30*time.Minute },
			rules: []WeeklyRule{monRule(9, 11)},
			want:  []string{"06-02 09:00", "06-02 09:30", "06-02 10:00"}},
		{name: "step par défaut 15 min",
			mod:   func(r *Request) { r.Step = 0; r.Duration = 30 * time.Minute },
			rules: []WeeklyRule{monRule(9, 10)},
			want:  []string{"06-02 09:00", "06-02 09:15", "06-02 09:30"}},
		{name: "plages qui se chevauchent : pas de doublon",
			rules: []WeeklyRule{monRule(9, 11), monRule(10, 12)},
			want:  hours("06-02", 9, 12)},
		{name: "ouverture + règle se recouvrent : pas de doublon", rules: []WeeklyRule{monRule(9, 11)},
			exc:  []Exception{{at(2025, 6, 2, 10, 0), at(2025, 6, 2, 12, 0), Open}},
			want: hours("06-02", 9, 12)},
		{name: "absence prime sur ouverture", rules: nil,
			exc: []Exception{
				{at(2025, 6, 2, 9, 0), at(2025, 6, 2, 12, 0), Open},
				{at(2025, 6, 2, 10, 0), at(2025, 6, 2, 11, 0), Closed}},
			want: []string{"06-02 09:00", "06-02 11:00"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := baseReq()
			if tc.mod != nil {
				tc.mod(&req)
			}
			got := fmtSlots(Slots(req, tc.rules, tc.exc, tc.busy))
			if !eq(got, tc.want) {
				t.Fatalf("got  %v\nwant %v", got, tc.want)
			}
		})
	}
}

func TestSlotsDST(t *testing.T) {
	t.Parallel()
	utc := func(ss []Slot) []string {
		out := make([]string, len(ss))
		for i, s := range ss {
			out[i] = s.Start.UTC().Format("01-02T15:04Z")
			if s.End.Sub(s.Start) != time.Hour {
				t.Errorf("durée réelle %v != 1h", s.End.Sub(s.Start))
			}
		}
		return out
	}
	rule := func(wd time.Weekday, sh, eh int) WeeklyRule {
		return WeeklyRule{Weekday: wd, Start: lt(sh, 0), End: lt(eh, 0)}
	}
	base := func(from, to time.Time) Request {
		return Request{Loc: paris, From: from, To: to, Now: at(2025, 1, 1, 0, 0), Duration: time.Hour, Step: time.Hour}
	}

	t.Run("dimanche 30/03/2025 : heure inexistante, 01:00-05:00 murale = 3h réelles", func(t *testing.T) {
		t.Parallel()
		got := utc(Slots(base(at(2025, 3, 30, 0, 0), at(2025, 3, 31, 0, 0)), []WeeklyRule{rule(time.Sunday, 1, 5)}, nil, nil))
		// 01:00 CET, 03:00 CEST, 04:00 CEST ; aucun créneau "02:00" fantôme.
		want := []string{"03-30T00:00Z", "03-30T01:00Z", "03-30T02:00Z"}
		if !eq(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("dimanche 26/10/2025 : heure doublée, 01:00-05:00 murale = 5h réelles", func(t *testing.T) {
		t.Parallel()
		got := utc(Slots(base(at(2025, 10, 26, 0, 0), at(2025, 10, 27, 0, 0)), []WeeklyRule{rule(time.Sunday, 1, 5)}, nil, nil))
		want := []string{"10-25T23:00Z", "10-26T00:00Z", "10-26T01:00Z", "10-26T02:00Z", "10-26T03:00Z"}
		if !eq(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("semaine du changement d'heure de mars : le lundi garde 09:00 mural", func(t *testing.T) {
		t.Parallel()
		rules := []WeeklyRule{rule(time.Saturday, 9, 10), rule(time.Monday, 9, 10)}
		got := utc(Slots(base(at(2025, 3, 29, 0, 0), at(2025, 4, 1, 0, 0)), rules, nil, nil))
		want := []string{"03-29T08:00Z", "03-31T07:00Z"} // CET puis CEST
		if !eq(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("semaine du changement d'heure d'octobre", func(t *testing.T) {
		t.Parallel()
		rules := []WeeklyRule{rule(time.Saturday, 9, 10), rule(time.Monday, 9, 10)}
		got := utc(Slots(base(at(2025, 10, 25, 0, 0), at(2025, 10, 28, 0, 0)), rules, nil, nil))
		want := []string{"10-25T07:00Z", "10-27T08:00Z"} // CEST puis CET
		if !eq(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("jour du changement d'heure : busy en instants absolus", func(t *testing.T) {
		t.Parallel()
		busy := []Busy{{time.Date(2025, 10, 26, 0, 30, 0, 0, time.UTC), time.Date(2025, 10, 26, 1, 30, 0, 0, time.UTC)}}
		got := utc(Slots(base(at(2025, 10, 26, 0, 0), at(2025, 10, 27, 0, 0)), []WeeklyRule{rule(time.Sunday, 1, 5)}, nil, busy))
		want := []string{"10-25T23:00Z", "10-26T02:00Z", "10-26T03:00Z"}
		if !eq(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("From/To couvrant plusieurs jours à cheval sur le DST : 7 jours, pas de saut", func(t *testing.T) {
		t.Parallel()
		var rules []WeeklyRule
		for wd := time.Sunday; wd <= time.Saturday; wd++ {
			rules = append(rules, rule(wd, 9, 10))
		}
		got := Slots(base(at(2025, 3, 27, 0, 0), at(2025, 4, 3, 0, 0)), rules, nil, nil)
		if len(got) != 7 {
			t.Fatalf("got %d créneaux: %v", len(got), fmtSlots(got))
		}
		for _, s := range got {
			if s.Start.In(paris).Hour() != 9 {
				t.Errorf("créneau pas à 09:00 murale : %v", s.Start.In(paris))
			}
		}
	})
}

// --- fuzz ---

func FuzzSlots(f *testing.F) {
	f.Add(int64(1), uint8(3), uint8(2), uint8(30), uint8(10), uint8(5), uint8(15))
	f.Add(int64(42), uint8(10), uint8(5), uint8(60), uint8(0), uint8(0), uint8(30))
	f.Add(int64(7), uint8(0), uint8(0), uint8(15), uint8(15), uint8(15), uint8(0))
	f.Fuzz(func(t *testing.T, seed int64, nBusy, nExc, durMin, bbMin, baMin, stepMin uint8) {
		rng := rand.New(rand.NewSource(seed)) //nolint:gosec // déterministe, test
		from := at(2025, 3, 26, 0, 0)         // couvre le DST de mars
		if seed%2 == 0 {
			from = at(2025, 10, 22, 0, 0) // et d'octobre
		}
		to := from.AddDate(0, 0, 9)
		dur := time.Duration(int(durMin)%120+1) * time.Minute
		req := Request{
			Loc: paris, From: from, To: to, Now: from.Add(-time.Hour),
			Duration: dur, Step: time.Duration(stepMin%60) * time.Minute,
			BufferBefore: time.Duration(bbMin%45) * time.Minute,
			BufferAfter:  time.Duration(baMin%45) * time.Minute,
		}
		rules := []WeeklyRule{}
		for wd := time.Sunday; wd <= time.Saturday; wd++ {
			if rng.Intn(3) == 0 {
				continue
			}
			s := rng.Intn(24 * 60)
			e := s + rng.Intn(24*60-s+1)
			rules = append(rules, WeeklyRule{Weekday: wd, Start: LocalTime(s), End: LocalTime(e)})
		}
		randIv := func() (time.Time, time.Time) {
			s := from.Add(time.Duration(rng.Intn(9*24*60)) * time.Minute)
			return s, s.Add(time.Duration(rng.Intn(600)+1) * time.Minute)
		}
		var exc []Exception
		var busy []Busy
		for i := 0; i < int(nExc)%8; i++ {
			s, e := randIv()
			exc = append(exc, Exception{s, e, ExceptionKind(rng.Intn(2))})
		}
		for i := 0; i < int(nBusy)%30; i++ {
			s, e := randIv()
			busy = append(busy, Busy{s, e})
		}

		got := Slots(req, rules, exc, busy)

		// Plages ouvertes recalculées indépendamment (heures murales par jour).
		var open []Interval
		for d := from; d.Before(to.AddDate(0, 0, 1)); d = d.AddDate(0, 0, 1) {
			for _, r := range rules {
				if r.Weekday == d.Weekday() {
					y, m, dd := d.Date()
					open = append(open, Interval{
						time.Date(y, m, dd, 0, int(r.Start), 0, 0, paris),
						time.Date(y, m, dd, 0, int(r.End), 0, 0, paris),
					})
				}
			}
		}
		for _, x := range exc {
			if x.Kind == Open {
				open = append(open, Interval{x.Start, x.End})
			}
		}
		openU := Union(open)

		for i, s := range got {
			if s.End.Sub(s.Start) != dur {
				t.Fatalf("durée %v != %v", s.End.Sub(s.Start), dur)
			}
			if i > 0 && !got[i-1].Start.Before(s.Start) {
				t.Fatalf("pas strictement trié/unique à %d", i)
			}
			if s.Start.Before(req.From) || !s.Start.Before(req.To) {
				t.Fatalf("hors [From,To): %v", s.Start)
			}
			soin := Interval(s)
			zs, ze := s.Start.Add(-req.BufferBefore), s.End.Add(req.BufferAfter)
			zone := Interval{zs, ze}
			contained := false
			for _, o := range openU {
				if !soin.Start.Before(o.Start) && !soin.End.After(o.End) {
					contained = true
				}
			}
			if !contained {
				t.Fatalf("soin %v hors plages ouvertes", soin)
			}
			for _, b := range busy {
				if zone.Overlaps(Interval(b)) {
					t.Fatalf("zone %v chevauche busy %v", zone, b)
				}
			}
			for _, x := range exc {
				if x.Kind == Closed && zone.Overlaps(Interval{x.Start, x.End}) {
					t.Fatalf("zone %v chevauche absence %v", zone, x)
				}
				if x.Kind == Closed && soin.Overlaps(Interval{x.Start, x.End}) {
					t.Fatalf("soin dans une absence")
				}
			}
		}
	})
}

// --- benchmark ---

func BenchmarkSlots60Days500Busy(b *testing.B) {
	from := at(2025, 9, 1, 0, 0)
	req := Request{Loc: paris, From: from, To: from.AddDate(0, 0, 60), Now: from.Add(-time.Hour),
		Duration: 30 * time.Minute, BufferBefore: 5 * time.Minute, BufferAfter: 5 * time.Minute, Step: 15 * time.Minute}
	var rules []WeeklyRule
	for wd := time.Monday; wd <= time.Friday; wd++ {
		rules = append(rules,
			WeeklyRule{Weekday: wd, Start: lt(8, 0), End: lt(12, 0)},
			WeeklyRule{Weekday: wd, Start: lt(14, 0), End: lt(19, 0)})
	}
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // déterministe, test
	busy := make([]Busy, 500)
	for i := range busy {
		s := from.AddDate(0, 0, rng.Intn(60)).Add(time.Duration(8*60+rng.Intn(10*60)) * time.Minute)
		busy[i] = Busy{s, s.Add(40 * time.Minute)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Slots(req, rules, nil, busy)
	}
}
