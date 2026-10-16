package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

type recSink struct {
	mu   sync.Mutex
	kind []string
}

func (r *recSink) Emit(_ context.Context, e service.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kind = append(r.kind, e.Kind)
}

func (r *recSink) kinds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.kind...)
}

type calFixture struct {
	fixture
	cal    *service.Calendar
	sink   *recSink
	typeID uuid.UUID // 30 min, 10 min de buffer après
}

func newCalFixture(t *testing.T) calFixture {
	t.Helper()
	f := newFixture(t)
	sink := &recSink{}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	cal := &service.Calendar{Pool: f.db, Clock: clock, Events: sink, ManageKey: service.NewManageKey(testutil.ManageSecret)}
	ctx := context.Background()
	if err := f.svc.AddRules(ctx, f.act, service.RuleInput{
		Weekdays: []string{"0", "1", "2", "3", "4", "6"}, Start: "08:00", End: "12:00", TypeIDs: allTypeIDs(t, f),
	}); err != nil {
		t.Fatal(err)
	}
	ty, err := sqlcgen.New(f.db).CreateAppointmentType(ctx, sqlcgen.CreateAppointmentTypeParams{
		PracticeID: f.act.PracticeID, Name: "Séance tampon",
		Duration:     pgtype.Interval{Microseconds: (30 * time.Minute).Microseconds(), Valid: true},
		BufferBefore: pgtype.Interval{Valid: true},
		BufferAfter:  pgtype.Interval{Microseconds: (10 * time.Minute).Microseconds(), Valid: true},
		Color:        "#2563eb",
	})
	if err != nil {
		t.Fatal(err)
	}
	return calFixture{fixture: f, cal: cal, sink: sink, typeID: ty.ID}
}

func (f calFixture) quick(date, clock string, force bool) service.NewAppointmentInput {
	return service.NewAppointmentInput{
		NewFirstName: "Léa", NewLastName: "Durand", NewPhone: "06 12 34 56 78",
		TypeID: f.typeID.String(), Date: date, Time: clock, Force: force,
	}
}

func TestCalendarLifecycle(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()

	first, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false))
	if err != nil {
		t.Fatalf("création: %v", err)
	}
	if first.Source != "staff" {
		t.Errorf("source %q", first.Source)
	}
	// Le patient a été créé avec un téléphone normalisé.
	patients, err := f.cal.SearchPatients(ctx, f.act, "durand")
	if err != nil || len(patients) != 1 || patients[0].Phone.String != "+33612345678" {
		t.Fatalf("recherche patient: %v %+v", err, patients)
	}

	// Chevauchement avec le buffer (09:00–09:30 + 10 min) : jamais accepté, même forcé.
	for _, force := range []bool{false, true} {
		in := f.quick("2026-10-19", "09:35", force)
		in.NewPhone = ""
		in.NewEmail = "x@example.test"
		if _, err := f.cal.Create(ctx, f.act, in); !errors.Is(err, domain.ErrSlotTaken) {
			t.Errorf("chevauchement (force=%v): %v", force, err)
		}
	}
	// Adjacent au buffer : accepté.
	second, err := f.cal.Create(ctx, f.act, service.NewAppointmentInput{
		PatientID: patients[0].ID.String(), TypeID: f.typeID.String(), Date: "2026-10-19", Time: "09:40",
	})
	if err != nil {
		t.Fatalf("adjacent: %v", err)
	}

	// Hors horaires : refusé sauf forcé.
	_, err = f.cal.Create(ctx, f.act, service.NewAppointmentInput{PatientID: patients[0].ID.String(), TypeID: f.typeID.String(), Date: "2026-10-19", Time: "14:00"})
	if !strings.Contains(messages(err), "hors des horaires") {
		t.Errorf("hors horaires: %v", err)
	}
	if _, err := f.cal.Create(ctx, f.act, service.NewAppointmentInput{PatientID: patients[0].ID.String(), TypeID: f.typeID.String(), Date: "2026-10-19", Time: "14:00", Force: true}); err != nil {
		t.Errorf("hors horaires forcé: %v", err)
	}
	// Validation.
	_, err = f.cal.Create(ctx, f.act, service.NewAppointmentInput{TypeID: "x", Date: "20/10", Time: "9h"})
	if len(domain.FieldErrors(err)) < 3 {
		t.Errorf("validation: %v", err)
	}

	// Déplacement : sur un autre RDV refusé, libre accepté, journalisé.
	if err := f.cal.Move(ctx, f.act, first.ID, service.MoveInput{Date: "2026-10-19", Time: "09:20"}); !errors.Is(err, domain.ErrSlotTaken) {
		t.Errorf("déplacement en conflit: %v", err)
	}
	if err := f.cal.Move(ctx, f.act, first.ID, service.MoveInput{Date: "2026-10-20", Time: "10:00"}); err != nil {
		t.Errorf("déplacement: %v", err)
	}
	d, err := f.cal.Detail(ctx, f.act, first.ID)
	if err != nil || d.Start.Format("2006-01-02 15:04") != "2026-10-20 10:00" || d.End.Format("15:04") != "10:30" {
		t.Fatalf("détail après déplacement: %v %+v", err, d)
	}
	if len(d.History) != 2 || !strings.Contains(d.History[1].Text, "Déplacé") {
		t.Errorf("historique: %+v", d.History)
	}

	// Statut puis déplacement interdit.
	if err := f.cal.SetStatus(ctx, f.act, first.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if err := f.cal.Move(ctx, f.act, first.ID, service.MoveInput{Date: "2026-10-20", Time: "11:00"}); len(domain.FieldErrors(err)) == 0 {
		t.Errorf("déplacement d'un RDV honoré: %v", err)
	}
	if err := f.cal.SetStatus(ctx, f.act, first.ID, "weird"); len(domain.FieldErrors(err)) == 0 {
		t.Errorf("statut inconnu: %v", err)
	}

	// Annulation : libère le créneau, pas deux fois.
	if err := f.cal.Cancel(ctx, f.act, second.ID, "patient malade"); err != nil {
		t.Fatal(err)
	}
	if err := f.cal.Cancel(ctx, f.act, second.ID, ""); len(domain.FieldErrors(err)) == 0 {
		t.Errorf("double annulation: %v", err)
	}
	d, _ = f.cal.Detail(ctx, f.act, second.ID)
	if d.Status != "cancelled" || d.CancelReason != "patient malade" || d.CanMove {
		t.Errorf("détail annulé: %+v", d)
	}
	if _, err := f.cal.Create(ctx, f.act, service.NewAppointmentInput{PatientID: patients[0].ID.String(), TypeID: f.typeID.String(), Date: "2026-10-19", Time: "09:40"}); err != nil {
		t.Errorf("créneau libéré par l'annulation: %v", err)
	}

	want := []string{
		service.EventAppointmentCreated, service.EventAppointmentCreated, service.EventAppointmentCreated,
		service.EventAppointmentMoved, service.EventAppointmentStatus, service.EventAppointmentCancelled,
		service.EventAppointmentCreated,
	}
	if got := f.sink.kinds(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("événements %v, attendu %v", got, want)
	}
}

func TestCalendarOtherPracticeIsNotFound(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	ap, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false))
	if err != nil {
		t.Fatal(err)
	}
	u, err := auth.CreateUser(ctx, f.db, auth.CreateUserParams{
		Email: "autre@example.test", Role: domain.RoleAdmin, PracticeName: "Autre cabinet", Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	other := service.Actor{UserID: u.ID, PracticeID: u.PracticeID}
	checks := map[string]error{
		"détail": func() error { _, err := f.cal.Detail(ctx, other, ap.ID); return err }(),
		"move":   f.cal.Move(ctx, other, ap.ID, service.MoveInput{Date: "2026-10-20", Time: "10:00", Force: true}),
		"cancel": f.cal.Cancel(ctx, other, ap.ID, ""),
		"status": f.cal.SetStatus(ctx, other, ap.ID, "done"),
	}
	for name, err := range checks {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s : %v, attendu ErrNotFound", name, err)
		}
	}
	// Le patient de l'autre cabinet n'est pas utilisable ni trouvable.
	if ps, _ := f.cal.SearchPatients(ctx, other, "durand"); len(ps) != 0 {
		t.Errorf("recherche inter-cabinets: %+v", ps)
	}
	patients, _ := f.cal.SearchPatients(ctx, f.act, "durand")
	_, err = f.cal.Create(ctx, other, service.NewAppointmentInput{PatientID: patients[0].ID.String(), TypeID: f.typeID.String(), Date: "2026-10-19", Time: "09:00", Force: true})
	if len(domain.FieldErrors(err)) == 0 {
		t.Errorf("patient d'un autre cabinet: %v", err)
	}
}

func TestCalendarViewDST(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	patientID := func() string {
		ap, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "08:00", false))
		if err != nil {
			t.Fatal(err)
		}
		return ap.PatientID.String()
	}()
	// Dimanche 25 octobre 2026 (retour à l'heure d'hiver) et dimanche 29 mars 2026
	// (passage à l'heure d'été) : un RDV à 09:00 mural doit tomber ligne 10
	// ((9-7) h × 4 lignes + en-tête), comme n'importe quel autre jour.
	for _, date := range []string{"2026-10-25", "2026-03-29"} {
		if _, err := f.cal.Create(ctx, f.act, service.NewAppointmentInput{PatientID: patientID, TypeID: f.typeID.String(), Date: date, Time: "09:00", Force: true}); err != nil {
			t.Fatalf("%s: %v", date, err)
		}
		v, err := f.cal.View(ctx, f.act, service.ViewInput{Mode: "week", Date: date})
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Days) != 7 || v.Days[6].DateStr != date || v.Days[0].Date.Weekday() != time.Monday {
			t.Fatalf("%s: semaine %v → %v", date, v.First, v.Last)
		}
		sun := v.Days[6]
		if len(sun.Appts) != 1 || sun.Appts[0].Row != 10 || sun.Appts[0].Span != 2 || sun.Appts[0].Start.Hour() != 9 {
			t.Errorf("%s: RDV mal placé %+v", date, sun.Appts)
		}
	}
	// Bandes grisées du dimanche 25 octobre (plage 08–12) : 07–08 et 12–21.
	v, _ := f.cal.View(ctx, f.act, service.ViewInput{Mode: "week", Date: "2026-10-25"})
	bands := v.Days[6].Bands
	if len(bands) != 2 || bands[0] != (service.CalBand{Row: 2, Span: 4}) || bands[1] != (service.CalBand{Row: 22, Span: 36}) {
		t.Errorf("bandes du dimanche DST: %+v", bands)
	}
	// Samedi sans plage : tout grisé.
	if sat := v.Days[5].Bands; len(sat) != 1 || sat[0].Span != 56 {
		t.Errorf("bandes du samedi: %+v", sat)
	}
	// La semaine du 19 octobre : le RDV de 08:00 du lundi, et le jour d'avant/après.
	if v.PrevDate != "2026-10-18" || v.NextDate != "2026-11-01" {
		t.Errorf("navigation %s %s", v.PrevDate, v.NextDate)
	}
}

func TestCalendarDayViewNowLineAndWindow(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	// 2026-10-15 09:00 UTC = 11:00 à Paris : ligne « maintenant » à (11-7)×4 + 2.
	v, err := f.cal.View(ctx, f.act, service.ViewInput{Mode: "day", Date: "2026-10-15"})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Days) != 1 || !v.Days[0].Today || v.Days[0].NowRow != 18 || v.Rows != 56 {
		t.Errorf("jour courant: %+v rows=%d", v.Days[0].NowRow, v.Rows)
	}
	if v2, _ := f.cal.View(ctx, f.act, service.ViewInput{Mode: "day", Date: "2026-10-16"}); v2.Days[0].NowRow != 0 {
		t.Errorf("ligne « maintenant » un autre jour")
	}
	// Un RDV tôt/tard élargit la fenêtre au lieu d'être caché.
	if _, err := f.cal.Create(ctx, f.act, f.quick("2026-10-16", "06:30", true)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.cal.Create(ctx, f.act, f.quick("2026-10-16", "22:00", true)); err != nil {
		t.Fatal(err)
	}
	v, _ = f.cal.View(ctx, f.act, service.ViewInput{Mode: "day", Date: "2026-10-16"})
	if v.FromHour != 6 || v.ToHour != 23 || len(v.Days[0].Appts) != 2 {
		t.Errorf("fenêtre %d–%d, %d RDV", v.FromHour, v.ToHour, len(v.Days[0].Appts))
	}
	if _, err := f.cal.View(ctx, f.act, service.ViewInput{Date: "pas-une-date"}); len(domain.FieldErrors(err)) == 0 {
		t.Errorf("date invalide: %v", err)
	}
}

func TestSearchPatientsEscapesWildcards(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	if _, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"%", "_", "d%", "x"} {
		if ps, err := f.cal.SearchPatients(ctx, f.act, q); err != nil || len(ps) != 0 {
			t.Errorf("%q : %v %+v", q, err, ps)
		}
	}
	for _, q := range []string{"Du", "léa dur", "+3361"} {
		if ps, err := f.cal.SearchPatients(ctx, f.act, q); err != nil || len(ps) != 1 {
			t.Errorf("%q : %v %+v", q, err, ps)
		}
	}
}
