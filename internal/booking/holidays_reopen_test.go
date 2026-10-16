package booking_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/booking"
)

func TestHolidayPartiallyReopened(t *testing.T) {
	t.Parallel()
	// Lundi de Pâques 2026 (06/04) : plage 9 h-18 h, ouverture exceptionnelle 14 h-18 h.
	rules := []booking.WeeklyRule{{Weekday: time.Monday, Start: booking.NewLocalTime(9, 0), End: booking.NewLocalTime(18, 0)}}
	req := booking.Request{
		Loc: parisTZ, From: time.Date(2026, 4, 6, 0, 0, 0, 0, parisTZ), To: time.Date(2026, 4, 7, 0, 0, 0, 0, parisTZ),
		Now: time.Date(2026, 3, 1, 0, 0, 0, 0, parisTZ), Duration: 30 * time.Minute, TypeID: uuid.New(),
	}
	run := func(openStart, openEnd int) []booking.Slot {
		o := booking.Interval{Start: time.Date(2026, 4, 6, openStart, 0, 0, 0, parisTZ), End: time.Date(2026, 4, 6, openEnd, 0, 0, 0, parisTZ)}
		exc := append(booking.HolidayClosures(parisTZ, req.From, req.To, false, []booking.Interval{o}),
			booking.Exception{Start: o.Start, End: o.End, Kind: booking.Open})
		return booking.Slots(req, rules, exc, nil)
	}
	got := run(14, 18)
	if len(got) == 0 {
		t.Fatal("aucun créneau l'après-midi")
	}
	for _, s := range got {
		if h := s.Start.In(parisTZ).Hour(); h < 14 {
			t.Errorf("créneau le matin du férié : %v", s.Start)
		}
	}
	if first := got[0].Start.In(parisTZ); first.Hour() != 14 || first.Minute() != 0 {
		t.Errorf("premier créneau %v, attendu 14 h", first)
	}
	// Ouverture couvrant toute la journée : comme sans férié.
	whole := booking.Exception{Start: time.Date(2026, 4, 6, 0, 0, 0, 0, parisTZ), End: time.Date(2026, 4, 7, 0, 0, 0, 0, parisTZ), Kind: booking.Open}
	want := booking.Slots(req, rules, []booking.Exception{whole}, nil)
	if all := run(0, 24); len(all) != len(want) {
		t.Errorf("journée entière rouverte : %d créneaux, attendu %d", len(all), len(want))
	}
}
