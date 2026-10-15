package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

func minutes(m int) pgtype.Interval {
	return pgtype.Interval{Microseconds: int64(m) * time.Minute.Microseconds(), Valid: true}
}

// newHomeEnv ajoute un motif à domicile (30 min, trajet 15 min) réservable en ligne
// dans les zones données ; env.typ devient ce motif.
func newHomeEnv(t *testing.T, enabled bool, zones []string) *bookingEnv {
	t.Helper()
	e := newBookingEnv(t, thursday)
	ctx := context.Background()
	q := sqlcgen.New(e.db)
	typ, err := q.CreateAppointmentType(ctx, sqlcgen.CreateAppointmentTypeParams{
		PracticeID: e.pc.ID, Name: "Séance à domicile", Duration: minutes(30),
		BufferBefore: minutes(0), BufferAfter: minutes(0), OnlineBookable: true, AtHome: true, Color: "#b45309",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.SetAppointmentTypeTravelBuffer(ctx, sqlcgen.SetAppointmentTypeTravelBufferParams{
		ID: typ.ID, PracticeID: e.pc.ID, TravelBuffer: minutes(15),
	}); err != nil {
		t.Fatal(err)
	}
	if err := q.SetPracticeHomeVisit(ctx, sqlcgen.SetPracticeHomeVisitParams{ID: e.pc.ID, HomeVisitOnline: enabled, HomeVisitZones: zones}); err != nil {
		t.Fatal(err)
	}
	e.typ, err = q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: typ.ID, PracticeID: e.pc.ID})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestHomeVisitTravelBlocksAgenda(t *testing.T) {
	t.Parallel()
	e := newHomeEnv(t, true, []string{"75011"})
	p := patient("Alice", "Durand", "06 11 11 11 11")
	p.Address = "12 rue de la Paix, 75011 Paris"
	at10 := tuesday9.Add(time.Hour)
	in := e.start(t, at10, p, "+33611111111")
	res, err := e.svc.Book(context.Background(), in)
	if err != nil {
		t.Fatalf("Book: %v", err)
	}

	// Le trajet est copié sur le RDV et inclus dans la zone bloquée.
	a := res.Appointment
	if got := a.BufferBefore.Microseconds; got != 15*time.Minute.Microseconds() {
		t.Errorf("buffer_before = %v µs", got)
	}
	if !a.DuringBlocked.Lower.Time.Equal(at10.Add(-15*time.Minute)) || !a.DuringBlocked.Upper.Time.Equal(at10.Add(45*time.Minute)) {
		t.Errorf("during_blocked = %v–%v", a.DuringBlocked.Lower.Time, a.DuringBlocked.Upper.Time)
	}
	if a.Address.String != p.Address {
		t.Error("adresse du RDV non enregistrée")
	}

	// Moteur de créneaux : les créneaux dont le trajet touche le RDV disparaissent.
	slots, err := e.svc.AvailableSlots(context.Background(), e.pr.ID, e.typ.ID, tuesday9, tuesday9.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, s := range slots {
		got[s.Start.Unix()] = true
	}
	for _, off := range []time.Duration{30 * time.Minute, time.Hour, 90 * time.Minute} { // 9 h 30, 10 h, 10 h 30
		if got[tuesday9.Add(off).Unix()] {
			t.Errorf("créneau %v encore offert malgré le trajet", tuesday9.Add(off).In(paris).Format("15:04"))
		}
	}
	if !got[tuesday9.Add(2*time.Hour).Unix()] { // 11 h : le trajet de part et d'autre ne touche plus le RDV
		t.Errorf("11 h devrait rester réservable, créneaux : %v", got)
	}
}

// Test de contrainte en base : le trajet est dans during_blocked, donc protégé par EXCLUDE.
func TestHomeVisitTravelProtectedByConstraint(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t)
	pc := testutil.NewPractice(t, db)
	pr := testutil.NewPractitioner(t, db, pc.ID)
	pat := testutil.NewPatient(t, db, pc.ID)
	typ := testutil.NewAppointmentType(t, db, pc.ID)
	ctx := context.Background()
	q := sqlcgen.New(db)
	start := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	create := func(s time.Time) error {
		bb, ba := domain.HomeBuffers(true, 0, 0, 15*time.Minute)
		_, err := store.CreateAppointment(ctx, q, store.NewAppointment{
			PracticeID: pc.ID, PractitionerID: pr.ID, PatientID: pat.ID, AppointmentTypeID: typ.ID,
			Start: s, End: s.Add(30 * time.Minute), BufferBefore: bb, BufferAfter: ba, Source: "staff", Address: "1 rue X 75011 Paris",
		})
		return err
	}
	if err := create(start); err != nil {
		t.Fatal(err)
	}
	// Soin 8 h–8 h 30, bloqué 7 h 45–8 h 45 : un autre RDV à 8 h 30 ou 8 h 59 (trajet avant compris) le touche.
	for _, s := range []time.Time{start.Add(30 * time.Minute), start.Add(59 * time.Minute)} {
		if err := create(s); !errors.Is(err, domain.ErrSlotTaken) {
			t.Errorf("RDV à %v : %v, attendu ErrSlotTaken", s.Format("15:04"), err)
		}
	}
	// 9 h 00 : blocage 8 h 45–9 h 45, qui touche sans chevaucher (intervalles [) ).
	if err := create(start.Add(time.Hour)); err != nil {
		t.Errorf("RDV à 9 h refusé: %v", err)
	}
}

func TestHomeVisitZoneEnforced(t *testing.T) {
	t.Parallel()
	e := newHomeEnv(t, true, []string{"75011", "Saint-Ouen"})
	ctx := context.Background()
	at := tuesday9.Add(time.Hour)

	for name, addr := range map[string]string{
		"hors zone": "5 quai de la Saône, 69001 Lyon",
		"vide":      "",
	} {
		p := patient("Alice", "Durand", "06 11 11 11 11")
		p.Address = addr
		_, err := e.svc.StartVerification(ctx, e.verifyInput(at, p))
		fe := domain.FieldErrors(err)
		if len(fe) != 1 || fe[0].Field != "address" {
			t.Errorf("%s : %v, attendu une erreur sur address", name, err)
		}
	}
	if e.otp.sent != 0 {
		t.Errorf("%d codes envoyés pour une adresse refusée", e.otp.sent)
	}
	p := patient("Alice", "Durand", "06 11 11 11 11")
	p.Address = "3 rue Y, 93400 SAINT-OUEN"
	if _, err := e.svc.StartVerification(ctx, e.verifyInput(at, p)); err != nil {
		t.Errorf("adresse en zone refusée: %v", err)
	}
}

func TestHomeVisitNotBookableWhenDisabled(t *testing.T) {
	t.Parallel()
	for name, e := range map[string]*bookingEnv{
		"option désactivée": newHomeEnv(t, false, []string{"75011"}),
		"aucune zone":       newHomeEnv(t, true, []string{}),
	} {
		p := patient("Alice", "Durand", "06 11 11 11 11")
		p.Address = "1 rue X 75011 Paris"
		if _, err := e.svc.StartVerification(context.Background(), e.verifyInput(tuesday9.Add(time.Hour), p)); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s : %v, attendu ErrNotFound", name, err)
		}
		page, err := e.svc.Practitioner(context.Background(), e.pc.Slug, e.pr.Slug)
		if err != nil {
			t.Fatal(err)
		}
		for _, ty := range page.Types {
			if ty.AtHome {
				t.Errorf("%s : motif à domicile listé", name)
			}
		}
	}
}
