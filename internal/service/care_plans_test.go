package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
)

type planFixture struct {
	calFixture
	plans   *service.CarePlans
	patient sqlcgen.Patient
}

func newPlanFixture(t *testing.T) planFixture {
	t.Helper()
	f := newCalFixture(t)
	p, err := sqlcgen.New(f.db).CreatePatient(context.Background(), sqlcgen.CreatePatientParams{
		PracticeID: f.act.PracticeID, FirstName: "Paul", LastName: "Martin",
		Phone: pgtype.Text{String: "+33612345678", Valid: true}, SmsOptIn: true, EmailOptIn: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	plans := &service.CarePlans{Pool: f.db, Clock: f.cal.Clock, Events: f.sink}
	return planFixture{calFixture: f, plans: plans, patient: p}
}

func (f planFixture) input() service.PlanInput {
	return service.PlanInput{
		PatientID: f.patient.ID.String(), TypeID: f.typeID.String(), Label: "Rééducation genou", Prescribed: "10", PerWeek: "2",
		Weekdays: []string{"0", "3"}, StartDate: "2026-10-19",
	}
}

func (f planFixture) lines(t *testing.T, prop service.Proposal) []service.LineInput {
	t.Helper()
	out := make([]service.LineInput, len(prop.Lines))
	for i, l := range prop.Lines {
		out[i] = service.LineInput{Date: l.Date, Time: l.Time}
	}
	return out
}

func (f planFixture) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCarePlanProposeAndCreate(t *testing.T) {
	t.Parallel()
	f := newPlanFixture(t)
	ctx := context.Background()

	prop, err := f.plans.Propose(ctx, f.act, f.input())
	if err != nil {
		t.Fatal(err)
	}
	if prop.Found != 10 || len(prop.Lines) != 10 {
		t.Fatalf("proposition : %d trouvées sur %d lignes", prop.Found, len(prop.Lines))
	}
	var got []string
	for _, l := range prop.Lines {
		got = append(got, l.Date+" "+l.Time)
	}
	// Lundi et jeudi à 08:00 (la 1re plage libre), de part et d'autre du passage à l'heure d'hiver (25/10).
	want := "2026-10-19 08:00,2026-10-22 08:00,2026-10-26 08:00,2026-10-29 08:00,2026-11-02 08:00,2026-11-05 08:00,2026-11-09 08:00,2026-11-12 08:00,2026-11-16 08:00,2026-11-19 08:00"
	if strings.Join(got, ",") != want {
		t.Fatalf("proposition inattendue :\n%s\nattendu :\n%s", strings.Join(got, ","), want)
	}

	plan, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: f.input(), Lines: f.lines(t, prop)})
	if err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, "appointment WHERE care_plan_id IS NOT NULL"); n != 10 {
		t.Fatalf("%d rendez-vous rattachés, attendu 10", n)
	}
	if k := f.sink.kinds(); len(k) != 10 {
		t.Errorf("%d événements, attendu 10", len(k))
	}

	views, err := f.plans.ForPatient(ctx, f.act, f.patient.ID)
	if err != nil || len(views) != 1 {
		t.Fatalf("séries : %v %d", err, len(views))
	}
	v := views[0]
	if v.ID != plan.ID || v.Count != 10 || v.Booked != 10 || v.Remaining != 0 || v.Weekdays != "Lundi, Jeudi" {
		t.Errorf("vue inattendue : %+v", v)
	}

	// Décompte : une séance honorée, une annulée.
	if err := f.cal.SetStatus(ctx, f.act, v.Sessions[0].ID, "done"); err != nil {
		t.Fatal(err)
	}
	if err := f.cal.Cancel(ctx, f.act, v.Sessions[1].ID, ""); err != nil {
		t.Fatal(err)
	}
	if views, err = f.plans.ForPatient(ctx, f.act, f.patient.ID); err != nil {
		t.Fatal(err)
	}
	v = views[0]
	if v.Done != 1 || v.Booked != 8 || v.Count != 9 || v.Remaining != 1 || !v.NeedsMoreSessions {
		t.Errorf("décompte : %+v", v)
	}
	third := v.Sessions[2] // 26/10 : devient la séance n°2 puisque la 22/10 est annulée
	if third.Ordinal != 2 {
		t.Errorf("ordinal de la 3e ligne = %d, attendu 2", third.Ordinal)
	}
	b, err := f.plans.ForAppointment(ctx, f.act, third.ID)
	if err != nil || b == nil || b.Ordinal != 2 || b.Prescribed != 10 || b.Replacement != nil {
		t.Fatalf("badge : %v %+v", err, b)
	}

	// Séance annulée : le prochain créneau compatible est proposé (jeudi 22/10, libéré).
	b, err = f.plans.ForAppointment(ctx, f.act, v.Sessions[1].ID)
	if err != nil || b == nil || b.Replacement == nil {
		t.Fatalf("remplacement : %v %+v", err, b)
	}
	if b.Replacement.Date != "2026-10-22" || b.Replacement.Time != "08:00" {
		t.Errorf("remplacement proposé %s %s", b.Replacement.Date, b.Replacement.Time)
	}
	if _, err := f.plans.AddSession(ctx, f.act, plan.ID, b.Replacement.Date, b.Replacement.Time); err != nil {
		t.Fatal(err)
	}
	// Série complète : plus de séance en plus.
	if _, err := f.plans.AddSession(ctx, f.act, plan.ID, "2026-12-01", "08:00"); len(domain.FieldErrors(err)) != 1 {
		t.Errorf("série complète : %v", err)
	}

	// Un RDV sans série n'a pas de badge.
	plain, err := f.cal.Create(ctx, f.act, f.quick("2026-11-30", "09:00", false))
	if err != nil {
		t.Fatal(err)
	}
	if b, err := f.plans.ForAppointment(ctx, f.act, plain.ID); err != nil || b != nil {
		t.Errorf("RDV sans série : %v %+v", err, b)
	}
}

// Un conflit au milieu de la série annule tout : ni série, ni RDV créés.
func TestCarePlanCreateIsAtomic(t *testing.T) {
	t.Parallel()
	f := newPlanFixture(t)
	ctx := context.Background()

	prop, err := f.plans.Propose(ctx, f.act, f.input())
	if err != nil {
		t.Fatal(err)
	}
	// Un autre RDV occupe la séance n°4 (jeudi 29/10 à 08:00) après la proposition.
	blocker := f.quick("2026-10-29", "08:00", false)
	if _, err := f.cal.Create(ctx, f.act, blocker); err != nil {
		t.Fatal(err)
	}
	before := f.count(t, "appointment")

	_, err = f.plans.Create(ctx, f.act, service.CreateInput{Plan: f.input(), Lines: f.lines(t, prop)})
	var conflict *service.LineConflict
	if !errors.As(err, &conflict) || !errors.Is(err, domain.ErrSlotTaken) {
		t.Fatalf("attendu un conflit de ligne, obtenu %v", err)
	}
	if conflict.Row != 4 {
		t.Errorf("ligne en conflit = %d, attendu 4", conflict.Row)
	}
	if n := f.count(t, "care_plan"); n != 0 {
		t.Errorf("%d série(s) créée(s) malgré le conflit", n)
	}
	if n := f.count(t, "appointment"); n != before {
		t.Errorf("%d rendez-vous, attendu %d (aucune création partielle)", n, before)
	}

	// Ajustée (jeudi 29/10 à 08:45, après le RDV et son tampon), la série passe.
	lines := f.lines(t, prop)
	lines[3].Time = "08:45"
	if _, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: f.input(), Lines: lines}); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, "appointment"); n != before+10 {
		t.Errorf("%d rendez-vous, attendu %d", n, before+10)
	}
}

func TestCarePlanValidation(t *testing.T) {
	t.Parallel()
	f := newPlanFixture(t)
	ctx := context.Background()
	line := func(d, h string) []service.LineInput { return []service.LineInput{{Date: d, Time: h}} }

	tests := []struct {
		name  string
		edit  func(*service.PlanInput)
		lines []service.LineInput
		want  string
	}{
		{"nombre de séances", func(in *service.PlanInput) { in.Prescribed = "0" }, line("2026-10-19", "08:00"), "entre 1 et 100"},
		{"fréquence", func(in *service.PlanInput) { in.PerWeek = "9" }, line("2026-10-19", "08:00"), "1 à 7"},
		{"plage horaire", func(in *service.PlanInput) { in.WindowStart, in.WindowEnd = "12:00", "09:00" }, line("2026-10-19", "08:00"), "plage horaire"},
		{"aucune séance", func(*service.PlanInput) {}, line("", ""), "au moins une séance"},
		{"hors horaires", func(*service.PlanInput) {}, line("2026-10-19", "15:00"), "Séance 1 : ce créneau est hors des horaires"},
		{"dans le passé", func(*service.PlanInput) {}, line("2026-10-12", "08:00"), "dans le futur"},
		{"plus que prescrit", func(in *service.PlanInput) { in.Prescribed = "1" }, []service.LineInput{{Date: "2026-10-19", Time: "08:00"}, {Date: "2026-10-22", Time: "08:00"}}, "plus de séances"},
		{"patient inconnu", func(in *service.PlanInput) { in.PatientID = uuid.NewString() }, line("2026-10-19", "08:00"), "Patient introuvable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := f.input()
			tt.edit(&in)
			_, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: in, Lines: tt.lines})
			if !strings.Contains(messages(err), tt.want) {
				t.Errorf("erreur %q, attendu %q", messages(err), tt.want)
			}
		})
	}
	if n := f.count(t, "care_plan"); n != 0 {
		t.Errorf("%d série(s) créée(s) par des saisies invalides", n)
	}
}

// Fusion de deux fiches : les séries suivent le patient conservé.
func TestCarePlanFollowsPatientMerge(t *testing.T) {
	t.Parallel()
	f := newPlanFixture(t)
	ctx := context.Background()
	prop, err := f.plans.Propose(ctx, f.act, f.input())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: f.input(), Lines: f.lines(t, prop)}); err != nil {
		t.Fatal(err)
	}
	keep, err := sqlcgen.New(f.db).CreatePatient(ctx, sqlcgen.CreatePatientParams{
		PracticeID: f.act.PracticeID, FirstName: "Paul", LastName: "Martin",
		Email: pgtype.Text{String: "paul@example.test", Valid: true}, SmsOptIn: true, EmailOptIn: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	patients := &service.Patients{Pool: f.db, Clock: f.cal.Clock}
	if _, err := patients.Merge(ctx, f.act, f.patient.ID, keep.ID); err != nil {
		t.Fatal(err)
	}
	if views, err := f.plans.ForPatient(ctx, f.act, keep.ID); err != nil || len(views) != 1 || views[0].Count != 10 {
		t.Errorf("série après fusion : %v %+v", err, views)
	}
}

func TestCarePlanSetStatus(t *testing.T) {
	t.Parallel()
	f := newPlanFixture(t)
	ctx := context.Background()
	prop, err := f.plans.Propose(ctx, f.act, f.input())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: f.input(), Lines: f.lines(t, prop)[:3]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.plans.SetStatus(ctx, f.act, plan.ID, "bogus"); len(domain.FieldErrors(err)) != 1 {
		t.Errorf("statut invalide : %v", err)
	}
	if p, err := f.plans.SetStatus(ctx, f.act, plan.ID, "completed"); err != nil || p.Status != "completed" {
		t.Fatalf("clôture : %v %+v", err, p)
	}
	// Série close : plus de séance ajoutable.
	if _, err := f.plans.AddSession(ctx, f.act, plan.ID, "2026-12-01", "08:00"); len(domain.FieldErrors(err)) != 1 {
		t.Errorf("ajout sur série close : %v", err)
	}
	if _, err := f.plans.SetStatus(ctx, f.act, uuid.New(), "completed"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("série inconnue : %v", err)
	}
}
