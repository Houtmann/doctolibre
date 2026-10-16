package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

type env struct {
	db       *pgxpool.Pool
	practice sqlcgen.Practice
	pract    sqlcgen.Practitioner
	patient  sqlcgen.Patient
	typ      sqlcgen.AppointmentType
}

func newEnv(t *testing.T) env {
	t.Helper()
	db := testutil.DB(t)
	p := testutil.NewPractice(t, db)
	return env{
		db: db, practice: p,
		pract:   testutil.NewPractitioner(t, db, p.ID),
		patient: testutil.NewPatient(t, db, p.ID),
		typ:     testutil.NewAppointmentType(t, db, p.ID),
	}
}

var day = time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

func at(h, m int) time.Time {
	return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
}

func (e env) book(practitioner uuid.UUID, start, end time.Time, after time.Duration) (sqlcgen.Appointment, error) {
	return store.CreateAppointment(context.Background(), sqlcgen.New(e.db), store.NewAppointment{
		PracticeID: e.practice.ID, PractitionerID: practitioner, PatientID: e.patient.ID,
		AppointmentTypeID: e.typ.ID, Start: start, End: end, BufferAfter: after, Source: "staff",
	})
}

func (e env) setStatus(id uuid.UUID, status string) error {
	_, err := sqlcgen.New(e.db).SetAppointmentStatus(context.Background(), sqlcgen.SetAppointmentStatusParams{
		ID: id, PracticeID: e.practice.ID, Status: status,
	})
	return err
}

func TestMigrateUpDownUp(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t) // up
	ctx := context.Background()
	count := func(table string) int {
		var n int
		q := "SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema()"
		args := []any{}
		if table != "" {
			q += " AND table_name = $1"
			args = append(args, table)
		}
		if err := db.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count("")
	// Redescend jusqu'à la migration outbox_message (les suivantes ne créent que des index).
	for i := 0; i < 40 && count("outbox_message") != 0; i++ {
		if err := store.MigrateDown(ctx, db); err != nil {
			t.Fatalf("down: %v", err)
		}
	}
	if n := count("outbox_message"); n != 0 {
		t.Fatalf("outbox_message doit avoir disparu: n=%d", n)
	}
	if n := count("appointment"); n != 1 {
		t.Fatalf("appointment ne doit pas être touchée: n=%d", n)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if again := count(""); again != before {
		t.Fatalf("up après down: %d tables, attendu %d", again, before)
	}
}

func TestAppointmentOverlap(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	other := testutil.NewPractitioner(t, e.db, e.practice.ID)
	if _, err := e.book(e.pract.ID, at(10, 0), at(10, 30), 0); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		practitioner uuid.UUID
		start, end   time.Time
		wantTaken    bool
	}{
		{"chevauchement", e.pract.ID, at(10, 15), at(10, 45), true},
		{"identique", e.pract.ID, at(10, 0), at(10, 30), true},
		{"adjacent apres", e.pract.ID, at(10, 30), at(11, 0), false},
		{"adjacent avant", e.pract.ID, at(9, 30), at(10, 0), false},
		{"autre praticien", other.ID, at(10, 0), at(10, 30), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.book(tc.practitioner, tc.start, tc.end, 0)
			if tc.wantTaken {
				if !errors.Is(err, domain.ErrSlotTaken) {
					t.Fatalf("attendu ErrSlotTaken, obtenu %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("attendu succès: %v", err)
			}
		})
	}
}

func TestAppointmentExclusionIsPgError(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if _, err := e.book(e.pract.ID, at(10, 0), at(10, 30), 0); err != nil {
		t.Fatal(err)
	}
	// Insertion brute (sans le wrapper) : l'erreur Postgres est bien 23P01.
	_, err := sqlcgen.New(e.db).CreateAppointment(context.Background(), sqlcgen.CreateAppointmentParams{
		PracticeID: e.practice.ID, PractitionerID: e.pract.ID, PatientID: e.patient.ID,
		AppointmentTypeID: e.typ.ID, StartsAt: at(10, 0), EndsAt: at(10, 30),
		BufferBefore: pgtype.Interval{Valid: true}, BufferAfter: pgtype.Interval{Valid: true},
		BlockedStart: at(10, 0), BlockedEnd: at(10, 30), Status: "booked", Source: "staff",
	})
	if !store.IsExclusionViolation(err) {
		t.Fatalf("attendu 23P01, obtenu %v", err)
	}
	if store.IsUniqueViolation(err) {
		t.Fatal("IsUniqueViolation ne doit pas matcher 23P01")
	}
}

func TestAppointmentCancelledDoesNotBlock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a, err := e.book(e.pract.ID, at(10, 0), at(10, 30), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.setStatus(a.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.book(e.pract.ID, at(10, 0), at(10, 30), 0); err != nil {
		t.Fatalf("créneau annulé doit être libre: %v", err)
	}
	// Réactiver l'annulé alors que le créneau est repris : refusé.
	if err := e.setStatus(a.ID, "booked"); !store.IsExclusionViolation(err) {
		t.Fatalf("réactivation en conflit doit échouer en 23P01: %v", err)
	}
}

func TestAppointmentBuffers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a, err := e.book(e.pract.ID, at(10, 0), at(10, 30), 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.DuringBlocked.Upper.Time.UTC(); !got.Equal(at(10, 45)) {
		t.Fatalf("during_blocked upper = %v, attendu 10:45", got)
	}
	if _, err := e.book(e.pract.ID, at(10, 30), at(11, 0), 0); !errors.Is(err, domain.ErrSlotTaken) {
		t.Fatalf("B 10:30 doit être rejeté (buffer), obtenu %v", err)
	}
	if _, err := e.book(e.pract.ID, at(10, 45), at(11, 15), 0); err != nil {
		t.Fatalf("B 10:45 doit être accepté: %v", err)
	}
}

func TestAppointmentBlockedContainsDuringCheck(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, err := sqlcgen.New(e.db).CreateAppointment(context.Background(), sqlcgen.CreateAppointmentParams{
		PracticeID: e.practice.ID, PractitionerID: e.pract.ID, PatientID: e.patient.ID,
		AppointmentTypeID: e.typ.ID, StartsAt: at(10, 0), EndsAt: at(10, 30),
		BufferBefore: pgtype.Interval{Valid: true}, BufferAfter: pgtype.Interval{Valid: true},
		BlockedStart: at(10, 5), BlockedEnd: at(10, 30), Status: "booked", Source: "staff",
	})
	if err == nil {
		t.Fatal("during_blocked ne contenant pas during doit être refusé")
	}
}

func TestAppointmentDoneNoShowStillBlock(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"done", "no_show"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			a, err := e.book(e.pract.ID, at(10, 0), at(10, 30), 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.setStatus(a.ID, status); err != nil {
				t.Fatal(err)
			}
			if _, err := e.book(e.pract.ID, at(10, 0), at(10, 30), 0); !errors.Is(err, domain.ErrSlotTaken) {
				t.Fatalf("%s doit bloquer le créneau, obtenu %v", status, err)
			}
		})
	}
}

func TestAppointmentConcurrentRace(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = e.book(e.pract.ID, at(14, 0), at(14, 30), 0)
		}()
	}
	close(start)
	wg.Wait()
	ok, taken := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrSlotTaken):
			taken++
		default:
			t.Errorf("erreur inattendue: %v", err)
		}
	}
	if ok != 1 || taken != n-1 {
		t.Fatalf("attendu 1 succès et %d ErrSlotTaken, obtenu %d / %d", n-1, ok, taken)
	}
}

func TestWithTxRollback(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	boom := errors.New("boom")
	err := store.WithTx(context.Background(), e.db, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		if _, err := store.CreateAppointment(context.Background(), q, store.NewAppointment{
			PracticeID: e.practice.ID, PractitionerID: e.pract.ID, PatientID: e.patient.ID,
			AppointmentTypeID: e.typ.ID, Start: at(9, 0), End: at(9, 30), Source: "staff",
		}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := e.book(e.pract.ID, at(9, 0), at(9, 30), 0); err != nil {
		t.Fatalf("rollback attendu, créneau libre: %v", err)
	}
}

func TestUniqueViolation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, err := sqlcgen.New(e.db).CreatePractice(context.Background(), sqlcgen.CreatePracticeParams{
		Name: "x", Slug: e.practice.Slug, Timezone: "Europe/Paris",
	})
	if !store.IsUniqueViolation(err) {
		t.Fatalf("attendu 23505, obtenu %v", err)
	}
}

func TestPatientNeedsContact(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	_, err := sqlcgen.New(e.db).CreatePatient(context.Background(), sqlcgen.CreatePatientParams{
		PracticeID: e.practice.ID, FirstName: "A", LastName: "B",
	})
	if err == nil {
		t.Fatal("patient sans téléphone ni email doit être refusé")
	}
}
