package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// setThreshold règle le seuil du cabinet (nil = désactivé).
func (e *bookingEnv) setThreshold(t *testing.T, n *int16) {
	t.Helper()
	v := pgtype.Int2{}
	if n != nil {
		v = pgtype.Int2{Int16: *n, Valid: true}
	}
	if err := sqlcgen.New(e.db).SetPracticeNoShowThreshold(context.Background(),
		sqlcgen.SetPracticeNoShowThresholdParams{ID: e.pc.ID, NoShowBlockThreshold: v}); err != nil {
		t.Fatal(err)
	}
}

// addNoShow ajoute un RDV manqué commençant à start pour le patient.
func (e *bookingEnv) addNoShow(t *testing.T, patientID uuid.UUID, start time.Time) {
	t.Helper()
	ap, err := store.CreateAppointment(context.Background(), sqlcgen.New(e.db), store.NewAppointment{
		ID: uuid.New(), PracticeID: e.pc.ID, PractitionerID: e.pr.ID, PatientID: patientID, AppointmentTypeID: e.typ.ID,
		Start: start, End: start.Add(30 * time.Minute), Source: "staff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(context.Background(), "UPDATE appointment SET status = 'no_show' WHERE id = $1", ap.ID); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineBookingNoShowGuard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	two := int16(2)
	now := thursday // 2026-10-15 09:00 UTC ; début de fenêtre : 2026-04-15 09:00 UTC
	edge := now.AddDate(0, -service.NoShowWindowMonths, 0)
	recent := now.AddDate(0, 0, -10)

	cases := []struct {
		name      string
		threshold *int16
		noShows   []time.Time
		blocked   bool
	}{
		{"seuil désactivé", nil, []time.Time{recent, recent.AddDate(0, 0, -10), recent.AddDate(0, 0, -20)}, false},
		{"seuil moins un", &two, []time.Time{recent}, false},
		{"seuil atteint", &two, []time.Time{recent, recent.AddDate(0, 0, -10)}, true},
		{"borne incluse", &two, []time.Time{recent, edge}, true},
		{"borne exclue (1 min plus ancien)", &two, []time.Time{recent, edge.Add(-time.Minute)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newBookingEnv(t, now)
			// Première réservation (seuil désactivé) : crée le patient.
			res, err := e.svc.Book(ctx, e.start(t, tuesday9, patient("Alice", "Durand", "06 11 11 11 11"), "+33611111111"))
			if err != nil {
				t.Fatalf("première réservation: %v", err)
			}
			pid := res.Appointment.PatientID
			for _, s := range tc.noShows {
				e.addNoShow(t, pid, s)
			}
			e.setThreshold(t, tc.threshold)

			next := tuesday9.AddDate(0, 0, 7)
			_, err = e.svc.Book(ctx, e.start(t, next, patient("Alice", "Durand", "06 11 11 11 11"), "+33611111111"))
			if tc.blocked {
				if !errors.Is(err, domain.ErrOnlineBookingBlocked) {
					t.Fatalf("Book = %v, attendu ErrOnlineBookingBlocked", err)
				}
				if n := e.count(t, "SELECT count(*) FROM appointment WHERE patient_id = $1 AND status = 'booked' AND lower(during) = $2", pid, next); n != 0 {
					t.Errorf("RDV créé malgré le refus")
				}
			} else if err != nil {
				t.Fatalf("Book = %v, attendu succès", err)
			}

			pat := &service.Patients{Pool: e.db, Clock: e.clock}
			d, err := pat.Detail(ctx, service.Actor{PracticeID: e.pc.ID}, pid)
			if err != nil {
				t.Fatal(err)
			}
			if d.OnlineBlocked != tc.blocked {
				t.Errorf("OnlineBlocked = %v, attendu %v", d.OnlineBlocked, tc.blocked)
			}
		})
	}
}
