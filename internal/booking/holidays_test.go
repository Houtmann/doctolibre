package booking_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/booking"
)

func d(y int, m time.Month, day int) booking.Date { return booking.Date{Year: y, Month: m, Day: day} }

func TestFrenchHolidaysEaster(t *testing.T) {
	t.Parallel()
	// Lundi de Pâques, Ascension (Pâques + 39 j) et lundi de Pentecôte (Pâques + 50 j).
	tests := []struct {
		year                   int
		easterMonday, asc, pen booking.Date
	}{
		{2024, d(2024, 4, 1), d(2024, 5, 9), d(2024, 5, 20)},
		{2025, d(2025, 4, 21), d(2025, 5, 29), d(2025, 6, 9)},
		{2026, d(2026, 4, 6), d(2026, 5, 14), d(2026, 5, 25)},
		{2027, d(2027, 3, 29), d(2027, 5, 6), d(2027, 5, 17)},
		{2038, d(2038, 4, 26), d(2038, 6, 3), d(2038, 6, 14)},
	}
	for _, tt := range tests {
		got := booking.FrenchHolidays(tt.year, false)
		if len(got) != 11 {
			t.Fatalf("%d : %d fériés, attendu 11", tt.year, len(got))
		}
		for _, want := range []booking.Date{tt.easterMonday, tt.asc, tt.pen} {
			if !slices.Contains(got, want) {
				t.Errorf("%d : %v absent de %v", tt.year, want, got)
			}
		}
		if !slices.IsSortedFunc(got, cmpDate) {
			t.Errorf("%d : non trié : %v", tt.year, got)
		}
	}
}

func TestFrenchHolidaysFixedAndAlsace(t *testing.T) {
	t.Parallel()
	got := booking.FrenchHolidays(2026, false)
	want := []booking.Date{
		d(2026, 1, 1), d(2026, 4, 6), d(2026, 5, 1), d(2026, 5, 8), d(2026, 5, 14), d(2026, 5, 25),
		d(2026, 7, 14), d(2026, 8, 15), d(2026, 11, 1), d(2026, 11, 11), d(2026, 12, 25),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("2026 = %v", got)
	}
	alsace := booking.FrenchHolidays(2026, true)
	if len(alsace) != 13 || !slices.Contains(alsace, d(2026, 4, 3)) || !slices.Contains(alsace, d(2026, 12, 26)) || !slices.IsSortedFunc(alsace, cmpDate) {
		t.Errorf("Alsace-Moselle 2026 = %v", alsace)
	}
	// Vendredi saint 2024 : 29/03 (Pâques le 31/03).
	if !slices.Contains(booking.FrenchHolidays(2024, true), d(2024, 3, 29)) {
		t.Error("Vendredi saint 2024 absent")
	}
	if slices.Contains(booking.FrenchHolidays(2024, false), d(2024, 3, 29)) {
		t.Error("Vendredi saint hors Alsace-Moselle")
	}
}

func cmpDate(a, b booking.Date) int {
	switch {
	case a.Year != b.Year:
		return a.Year - b.Year
	case a.Month != b.Month:
		return int(a.Month) - int(b.Month)
	}
	return a.Day - b.Day
}

func TestHolidaysBetweenAcrossYears(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 12, 24, 0, 0, 0, 0, parisTZ)
	to := time.Date(2027, 1, 2, 0, 0, 0, 0, parisTZ)
	got := booking.HolidaysBetween(parisTZ, from, to, true)
	want := []booking.Date{d(2026, 12, 25), d(2026, 12, 26), d(2027, 1, 1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestHolidayClosuresDST(t *testing.T) {
	t.Parallel()
	// 2024 : Pâques le 31/03 = passage à l'heure d'été ; le lundi 01/04 est férié en CEST.
	from, to := time.Date(2024, 3, 30, 0, 0, 0, 0, parisTZ), time.Date(2024, 4, 3, 0, 0, 0, 0, parisTZ)
	got := booking.HolidayClosures(parisTZ, from, to, false, nil)
	if len(got) != 1 || got[0].Kind != booking.Closed {
		t.Fatalf("closures = %v", got)
	}
	if !got[0].Start.Equal(time.Date(2024, 3, 31, 22, 0, 0, 0, time.UTC)) || !got[0].End.Equal(time.Date(2024, 4, 1, 22, 0, 0, 0, time.UTC)) {
		t.Errorf("journée = [%v, %v)", got[0].Start, got[0].End)
	}
}

func TestHolidayClosuresBlockEngine(t *testing.T) {
	t.Parallel()
	// Lundi de Pâques 2026 (06/04), plage du lundi 9 h-12 h.
	rules := []booking.WeeklyRule{{Weekday: time.Monday, Start: booking.NewLocalTime(9, 0), End: booking.NewLocalTime(12, 0)}}
	req := booking.Request{
		Loc: parisTZ, From: time.Date(2026, 4, 5, 0, 0, 0, 0, parisTZ), To: time.Date(2026, 4, 14, 0, 0, 0, 0, parisTZ),
		Now: time.Date(2026, 3, 1, 0, 0, 0, 0, parisTZ), Duration: 30 * time.Minute, TypeID: uuid.New(),
	}
	count := func(exc []booking.Exception, day int) int {
		n := 0
		for _, s := range booking.Slots(req, rules, exc, nil) {
			if s.Start.In(parisTZ).Day() == day {
				n++
			}
		}
		return n
	}
	if count(nil, 6) == 0 {
		t.Fatal("option désactivée : le lundi 06/04 doit avoir des créneaux")
	}
	closures := booking.HolidayClosures(parisTZ, req.From, req.To, false, nil)
	if count(closures, 6) != 0 {
		t.Error("jour férié : aucun créneau attendu")
	}
	if count(closures, 13) == 0 {
		t.Error("le lundi suivant n'est pas férié")
	}
	// Ouverture exceptionnelle explicite le même jour : pas de fermeture générée.
	openExc := booking.Exception{
		Start: time.Date(2026, 4, 6, 14, 0, 0, 0, parisTZ), End: time.Date(2026, 4, 6, 16, 0, 0, 0, parisTZ), Kind: booking.Open,
	}
	opens := []booking.Interval{{Start: openExc.Start, End: openExc.End}}
	exc := append(booking.HolidayClosures(parisTZ, req.From, req.To, false, opens), openExc)
	if got := count(exc, 6); got == 0 {
		t.Error("une ouverture exceptionnelle doit rouvrir le jour férié")
	}
}
