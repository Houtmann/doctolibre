package service_test

import (
	"context"
	"testing"
	"time"

	"doctolibre/internal/store/sqlcgen"
)

func TestSlotsClosedOnPublicHolidays(t *testing.T) {
	t.Parallel()
	// Le dimanche 1er novembre 2026 (Toussaint) : plage de 9 h à 10 h.
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	e := newBookingEnv(t, now)
	ctx := context.Background()
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, paris)
	slotsOn := func() int {
		got, err := e.svc.AvailableSlots(ctx, e.pr.ID, e.typ.ID, day, day.AddDate(0, 0, 1))
		if err != nil {
			t.Fatal(err)
		}
		return len(got)
	}
	if slotsOn() == 0 {
		t.Fatal("option désactivée : créneaux attendus")
	}
	if err := sqlcgen.New(e.db).SetPracticeHolidays(ctx, sqlcgen.SetPracticeHolidaysParams{ID: e.pc.ID, CloseOnHolidays: true}); err != nil {
		t.Fatal(err)
	}
	if n := slotsOn(); n != 0 {
		t.Errorf("férié : %d créneaux, attendu 0", n)
	}
	if err := e.svc.CheckSlot(ctx, e.pr.ID, e.typ.ID, time.Date(2026, 11, 1, 9, 0, 0, 0, paris)); err == nil {
		t.Error("CheckSlot doit refuser un créneau férié")
	}
	// Ouverture exceptionnelle explicite l'après-midi : le jour est rouvert.
	_, err := sqlcgen.New(e.db).CreateAvailabilityException(ctx, sqlcgen.CreateAvailabilityExceptionParams{
		PractitionerID: e.pr.ID, Kind: "open",
		StartsAt: time.Date(2026, 11, 1, 14, 0, 0, 0, paris), EndsAt: time.Date(2026, 11, 1, 16, 0, 0, 0, paris),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Seule l'ouverture explicite est réservable : rien à 9 h (plage hebdo), des créneaux à 14 h.
	got, err := e.svc.AvailableSlots(ctx, e.pr.ID, e.typ.ID, day, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("l'ouverture exceptionnelle doit rouvrir le jour férié")
	}
	for _, s := range got {
		if h := s.Start.In(paris).Hour(); h < 14 || h >= 16 {
			t.Errorf("créneau hors ouverture explicite : %v", s.Start)
		}
	}
}
