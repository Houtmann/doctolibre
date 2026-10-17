package booking

import (
	"testing"
	"time"
)

func TestSlotsResourceBusy(t *testing.T) {
	t.Parallel()
	rules := []WeeklyRule{monRule(9, 13)}

	t.Run("ressource prise : créneau retiré, buffers inclus", func(t *testing.T) {
		t.Parallel()
		req := baseReq()
		req.BufferAfter = 15 * time.Minute
		req.ResourceBusy = []Busy{{at(2025, 6, 2, 10, 0), at(2025, 6, 2, 11, 0)}}
		// 09:00 bloque [09:00,10:15) : chevauche la ressource ; 11:00 est libre ; 12:00 aussi.
		got := fmtSlots(Slots(req, rules, nil, nil))
		want := []string{"06-02 11:00", "06-02 12:00"}
		if !eq(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("sans ressource : inchangé", func(t *testing.T) {
		t.Parallel()
		got := fmtSlots(Slots(baseReq(), rules, nil, nil))
		if want := hours("06-02", 9, 13); !eq(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("DST mars : dimanche 30/03/2025", func(t *testing.T) {
		t.Parallel()
		req := Request{Loc: paris, From: at(2025, 3, 30, 0, 0), To: at(2025, 3, 31, 0, 0), Now: at(2025, 1, 1, 0, 0), Duration: time.Hour, Step: time.Hour,
			ResourceBusy: []Busy{{time.Date(2025, 3, 30, 1, 0, 0, 0, time.UTC), time.Date(2025, 3, 30, 2, 0, 0, 0, time.UTC)}}} // 02:00-03:00 CET/CEST... 03:00-04:00 mural
		r := []WeeklyRule{{Weekday: time.Sunday, Start: lt(1, 0), End: lt(5, 0)}}
		var got []string
		for _, s := range Slots(req, r, nil, nil) {
			got = append(got, s.Start.UTC().Format("15:04Z"))
		}
		want := []string{"00:00Z", "02:00Z"} // le créneau de 01:00Z est pris
		if !eq(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("DST octobre : dimanche 26/10/2025, heure doublée", func(t *testing.T) {
		t.Parallel()
		req := Request{Loc: paris, From: at(2025, 10, 26, 0, 0), To: at(2025, 10, 27, 0, 0), Now: at(2025, 1, 1, 0, 0), Duration: time.Hour, Step: time.Hour,
			ResourceBusy: []Busy{{time.Date(2025, 10, 26, 0, 0, 0, 0, time.UTC), time.Date(2025, 10, 26, 2, 0, 0, 0, time.UTC)}}}
		r := []WeeklyRule{{Weekday: time.Sunday, Start: lt(1, 0), End: lt(5, 0)}}
		var got []string
		for _, s := range Slots(req, r, nil, nil) {
			got = append(got, s.Start.UTC().Format("15:04Z"))
		}
		want := []string{"23:00Z", "02:00Z", "03:00Z"} // 00:00Z et 01:00Z pris
		if !eq(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
}
