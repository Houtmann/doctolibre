package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

// Saisie par le cabinet : adresse requise, trajet bloqué, zones non appliquées.
func TestStaffHomeVisitCreate(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	q := sqlcgen.New(f.db)
	ty, err := q.CreateAppointmentType(ctx, sqlcgen.CreateAppointmentTypeParams{
		PracticeID: f.act.PracticeID, Name: "Domicile", Duration: minutes(30),
		BufferBefore: minutes(0), BufferAfter: minutes(0), AtHome: true, Color: "#b45309",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.SetAppointmentTypeTravelBuffer(ctx, sqlcgen.SetAppointmentTypeTravelBufferParams{ID: ty.ID, PracticeID: f.act.PracticeID, TravelBuffer: minutes(20)}); err != nil {
		t.Fatal(err)
	}
	in := f.quick("2026-10-19", "09:00", false)
	in.TypeID = ty.ID.String()
	if _, err := f.cal.Create(ctx, f.act, in); len(domain.FieldErrors(err)) != 1 || domain.FieldErrors(err)[0].Field != "address" {
		t.Fatalf("sans adresse : %v", err)
	}
	in.Address = "8 chemin des Vignes, 13100 Aix"
	a, err := f.cal.Create(ctx, f.act, in)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.DuringBlocked.Upper.Time.Sub(a.DuringBlocked.Lower.Time); got != 70*time.Minute {
		t.Errorf("zone bloquée = %v, attendu 30 min + 2 x 20 min", got)
	}
	in.Time = "09:45" // dans le trajet de retour
	if _, err := f.cal.Create(ctx, f.act, in); !errors.Is(err, domain.ErrSlotTaken) {
		t.Errorf("RDV dans le trajet : %v", err)
	}
}
