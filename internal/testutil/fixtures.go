package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/store/sqlcgen"
)

const fixtureTimeout = 10 * time.Second

func fixtureCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), fixtureTimeout)
	t.Cleanup(cancel)
	return ctx
}

// NewPractice crée un cabinet (slug unique).
func NewPractice(t *testing.T, db *pgxpool.Pool) sqlcgen.Practice {
	t.Helper()
	p, err := sqlcgen.New(db).CreatePractice(fixtureCtx(t), sqlcgen.CreatePracticeParams{
		Name:     "Cabinet de test",
		Slug:     "cabinet-" + uuid.NewString(),
		Timezone: "Europe/Paris",
	})
	if err != nil {
		t.Fatalf("NewPractice: %v", err)
	}
	return p
}

// NewPractitioner crée un praticien actif dans le cabinet (slug unique).
func NewPractitioner(t *testing.T, db *pgxpool.Pool, practiceID uuid.UUID) sqlcgen.Practitioner {
	t.Helper()
	p, err := sqlcgen.New(db).CreatePractitioner(fixtureCtx(t), sqlcgen.CreatePractitionerParams{
		PracticeID: practiceID,
		FirstName:  "Camille",
		LastName:   "Martin",
		Slug:       "praticien-" + uuid.NewString(),
		Color:      "#2563eb",
	})
	if err != nil {
		t.Fatalf("NewPractitioner: %v", err)
	}
	return p
}

// NewPatient crée un patient avec un email unique.
func NewPatient(t *testing.T, db *pgxpool.Pool, practiceID uuid.UUID) sqlcgen.Patient {
	t.Helper()
	p, err := sqlcgen.New(db).CreatePatient(fixtureCtx(t), sqlcgen.CreatePatientParams{
		PracticeID: practiceID,
		FirstName:  "Alex",
		LastName:   "Dupont",
		Email:      pgtype.Text{String: "patient-" + uuid.NewString() + "@example.test", Valid: true},
		SmsOptIn:   true,
		EmailOptIn: true,
	})
	if err != nil {
		t.Fatalf("NewPatient: %v", err)
	}
	return p
}

// NewAppointmentType crée un motif de 30 min sans buffer.
func NewAppointmentType(t *testing.T, db *pgxpool.Pool, practiceID uuid.UUID) sqlcgen.AppointmentType {
	t.Helper()
	zero := pgtype.Interval{Valid: true}
	at, err := sqlcgen.New(db).CreateAppointmentType(fixtureCtx(t), sqlcgen.CreateAppointmentTypeParams{
		PracticeID:     practiceID,
		Name:           "Séance",
		Duration:       pgtype.Interval{Microseconds: (30 * time.Minute).Microseconds(), Valid: true},
		BufferBefore:   zero,
		BufferAfter:    zero,
		OnlineBookable: true,
		Color:          "#2563eb",
	})
	if err != nil {
		t.Fatalf("NewAppointmentType: %v", err)
	}
	return at
}

// ManageSecret est un DOCTOLIBRE_SECRET_KEY de test (32 octets) : les services
// qui créent des rendez-vous en dérivent ManageKey (service.NewManageKey).
const ManageSecret = "test-secret-key-0123456789abcdef"
