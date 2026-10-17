package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

// linkFixture : une série de 10 séances (à partir du lundi 19/10/2026), un patient
// joignable par email et un service public Booking pour lire /s/{jeton}.
type linkFixture struct {
	planFixture
	plan   sqlcgen.CarePlan
	token  string
	public *service.Booking
}

func newLinkFixture(t *testing.T) linkFixture {
	t.Helper()
	f := newPlanFixture(t)
	f.plans.BaseURL = "https://rdv.test"
	ctx := context.Background()
	mustExec(t, f.db, "UPDATE patient SET email = 'paul@example.test' WHERE id = $1", f.patient.ID)
	prop, err := f.plans.Propose(ctx, f.act, f.input())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: f.input(), Lines: f.lines(t, prop)})
	if err != nil {
		t.Fatal(err)
	}
	key := service.NewManageKey(testutil.ManageSecret)
	return linkFixture{
		planFixture: f, plan: plan, token: key.PlanToken(plan.ID, 1),
		public: &service.Booking{Pool: f.db, Clock: f.cal.Clock, ManageKey: key},
	}
}

func TestSeriesConfirmationCarriesPlanLinkOnce(t *testing.T) {
	t.Parallel()
	f := newLinkFixture(t)
	ms := outMsgs(t, f.db, f.act.PracticeID)
	if len(ms) != 1 || ms[0].Template != "series" || ms[0].Kind != "email" {
		t.Fatalf("messages %+v, attendu une seule confirmation de série", ms)
	}
	want := "https://rdv.test/s/" + f.token
	if !strings.Contains(ms[0].Payload, want) {
		t.Errorf("payload sans le lien de série %s : %s", want, ms[0].Payload)
	}
	if strings.Contains(ms[0].Payload, "/r/") {
		t.Errorf("la confirmation de série ne doit pas contenir de lien par séance : %s", ms[0].Payload)
	}
	// Seul le haché est stocké.
	var hash string
	if err := f.db.QueryRow(context.Background(), "SELECT token_hash FROM care_plan WHERE id = $1", f.plan.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != service.ManageTokenHash(f.token) || strings.Contains(hash, f.token) {
		t.Errorf("token_hash = %q", hash)
	}
	// Une fois le message envoyé, le lien est effacé du payload.
	mustExec(t, f.db, "UPDATE outbox_message SET status = 'sending', attempts = 1 WHERE practice_id = $1", f.act.PracticeID)
	var id uuid.UUID
	if err := f.db.QueryRow(context.Background(), "SELECT id FROM outbox_message WHERE practice_id = $1", f.act.PracticeID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	n, err := sqlcgen.New(f.db).MarkOutboxSent(context.Background(), sqlcgen.MarkOutboxSentParams{Now: f.cal.Clock.Now(), ID: id, Attempts: 1})
	if err != nil || n != 1 {
		t.Fatalf("MarkOutboxSent = %d, %v", n, err)
	}
	if after := outMsgs(t, f.db, f.act.PracticeID); strings.Contains(after[0].Payload, f.token) || strings.Contains(after[0].Payload, "series_url") {
		t.Errorf("le lien de série reste dans le payload envoyé : %s", after[0].Payload)
	}
}

func TestCarePlanByTokenListsSessions(t *testing.T) {
	t.Parallel()
	f := newLinkFixture(t)
	ctx := context.Background()
	p, err := f.public.CarePlanByToken(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Upcoming) != 10 || len(p.Past) != 0 || p.FirstName != "Paul" {
		t.Fatalf("upcoming=%d past=%d first=%q", len(p.Upcoming), len(p.Past), p.FirstName)
	}
	for i, s := range p.Upcoming {
		if s.ManageToken == "" {
			t.Errorf("séance %d à venir sans lien de gestion", i)
		}
	}
	// Une séance annulée et une séance honorée : sans lien, dans « passées ».
	mustExec(t, f.db, `UPDATE appointment SET status = 'cancelled', cancelled_at = now()
		WHERE id = $1`, firstSession(t, f, 0))
	mustExec(t, f.db, "UPDATE appointment SET status = 'done' WHERE id = $1", firstSession(t, f, 1))
	p, err = f.public.CarePlanByToken(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Upcoming) != 8 || len(p.Past) != 2 {
		t.Fatalf("upcoming=%d past=%d, attendu 8 et 2", len(p.Upcoming), len(p.Past))
	}
	for _, s := range p.Past {
		if s.ManageToken != "" {
			t.Errorf("séance %s (%s) avec un lien de gestion", s.Status, s.StatusLabel)
		}
	}
	// Séance dans le passé (horloge avancée) : plus de lien non plus.
	f.cal.Clock.(*testutil.FixedClock).Advance(time.Date(2026, 10, 27, 0, 0, 0, 0, paris).Sub(f.cal.Clock.Now()))
	p, err = f.public.CarePlanByToken(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Upcoming) != 7 {
		t.Errorf("upcoming=%d après le 27/10, attendu 7", len(p.Upcoming))
	}

	// Jetons falsifiés : 404 neutre.
	for _, bad := range []string{"", "x", f.token + "x", strings.ToUpper(f.token), strings.Repeat("a", 200), service.NewManageKey("autre").PlanToken(f.plan.ID, 1)} {
		if _, err := f.public.CarePlanByToken(ctx, bad); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("jeton %.12q : %v, attendu ErrNotFound", bad, err)
		}
	}
	// Un jeton de séance (/r/) n'ouvre pas la série.
	if _, err := f.public.CarePlanByToken(ctx, p.Upcoming[0].ManageToken); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("jeton de séance sur /s/ : %v", err)
	}
}

func TestCarePlanLinkRevokedByMerge(t *testing.T) {
	t.Parallel()
	f := newLinkFixture(t)
	ctx := context.Background()
	keep, err := sqlcgen.New(f.db).CreatePatient(ctx, sqlcgen.CreatePatientParams{
		PracticeID: f.act.PracticeID, FirstName: "Paul", LastName: "Martin",
		Email: pgtype.Text{String: "paul2@example.test", Valid: true}, SmsOptIn: true, EmailOptIn: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	patients := &service.Patients{Pool: f.db, Clock: f.cal.Clock}
	if _, err := patients.Merge(ctx, f.act, f.patient.ID, keep.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.public.CarePlanByToken(ctx, f.token); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("lien de série après fusion : %v, attendu ErrNotFound", err)
	}
	var version int
	var hash *string
	if err := f.db.QueryRow(ctx, "SELECT token_version, token_hash FROM care_plan WHERE id = $1", f.plan.ID).Scan(&version, &hash); err != nil {
		t.Fatal(err)
	}
	if version != 2 || hash != nil {
		t.Errorf("version=%d hash=%v, attendu 2 et NULL", version, hash)
	}
}

func TestCarePlanLinkRevokedByErasure(t *testing.T) {
	t.Parallel()
	f := newLinkFixture(t)
	ctx := context.Background()
	mustExec(t, f.db, "UPDATE appointment SET status = 'done' WHERE care_plan_id = $1", f.plan.ID)
	patients := &service.Patients{Pool: f.db, Clock: f.cal.Clock}
	if mode, err := patients.Erase(ctx, f.act, f.patient.ID); err != nil || mode != "anonymized" {
		t.Fatalf("Erase = %q, %v", mode, err)
	}
	if _, err := f.public.CarePlanByToken(ctx, f.token); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("lien de série après effacement : %v, attendu ErrNotFound", err)
	}
}

// Séries antérieures à T055 : haché NULL, renseigné à la première génération du lien.
func TestEnsurePlanTokenIsLazyForExistingPlans(t *testing.T) {
	t.Parallel()
	f := newLinkFixture(t)
	ctx := context.Background()
	mustExec(t, f.db, "UPDATE care_plan SET token_hash = NULL WHERE id = $1", f.plan.ID)
	if _, err := f.public.CarePlanByToken(ctx, f.token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("avant génération : %v", err)
	}
	if tok, err := service.EnsurePlanTokenForTest(ctx, f.db, service.NewManageKey(testutil.ManageSecret), f.plan.ID); err != nil || tok != f.token {
		t.Fatalf("génération : %q %v", tok, err)
	}
	if _, err := f.public.CarePlanByToken(ctx, f.token); err != nil {
		t.Errorf("après génération : %v", err)
	}
}

// firstSession retourne l'id de la n-ième séance (chronologique) de la série.
func firstSession(t *testing.T, f linkFixture, n int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.db.QueryRow(context.Background(),
		"SELECT id FROM appointment WHERE care_plan_id = $1 ORDER BY lower(during) OFFSET $2 LIMIT 1", f.plan.ID, n).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
