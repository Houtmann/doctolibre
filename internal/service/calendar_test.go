package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func isoRange(t *testing.T, f calFixture, start, end string) service.AgendaEvents {
	t.Helper()
	res, err := f.cal.AgendaRange(context.Background(), f.act, service.EventsInput{Start: start, End: end})
	if err != nil {
		t.Fatalf("AgendaRange(%s, %s): %v", start, end, err)
	}
	return res
}

func fmtRFC(t time.Time) string { return t.Format(time.RFC3339) }

// Semaines de changement d'heure (dimanche 25 octobre et 29 mars 2026, Europe/Paris) :
// un RDV à 09:00 mural reste à 09:00, avec le bon décalage ; les indisponibilités sont
// calculées jour par jour (dimanche de 25 h ou de 23 h comprises).
func TestAgendaRangeDST(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	// Les plages créées par la fixture valent à partir du « jour même » (15 octobre) : on les fait remonter pour mars.
	if _, err := f.db.Exec(ctx, "UPDATE availability_rule SET valid_from = DATE '2026-01-01'"); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ monday, sunday, nextMonday, offset, nextOffset string }{
		{"2026-10-19", "2026-10-25", "2026-10-26", "+02:00", "+01:00"},
		{"2026-03-23", "2026-03-29", "2026-03-30", "+01:00", "+02:00"},
	}
	for _, c := range cases {
		in := f.quick(c.sunday, "09:00", true)
		if _, err := f.cal.Create(ctx, f.act, in); err != nil {
			t.Fatalf("%s: %v", c.sunday, err)
		}
		// Les deux formes de dates : heure murale sans décalage, et RFC 3339 avec décalage.
		wall := isoRange(t, f, c.monday+"T00:00:00", c.nextMonday+"T00:00:00")
		abs := isoRange(t, f, c.monday+"T00:00:00"+c.offset, c.nextMonday+"T00:00:00"+c.nextOffset)
		if len(wall.Appts) != 1 || len(abs.Appts) != 1 {
			t.Fatalf("%s: %d / %d RDV", c.sunday, len(wall.Appts), len(abs.Appts))
		}
		ap := wall.Appts[0]
		if ap.Start.Hour() != 9 || ap.Start.Format("2006-01-02") != c.sunday || fmtRFC(ap.Start) != c.sunday+"T09:00:00"+c.nextOffset {
			t.Errorf("%s: RDV mal placé %s", c.sunday, fmtRFC(ap.Start))
		}
		// Buffer de 10 min après le soin de 30 min ; rien avant.
		if !ap.BlockedStart.Equal(ap.Start) || ap.BlockedEnd.Sub(ap.End) != 10*time.Minute || ap.End.Sub(ap.Start) != 30*time.Minute {
			t.Errorf("%s: buffers %s %s %s %s", c.sunday, fmtRFC(ap.Start), fmtRFC(ap.End), fmtRFC(ap.BlockedStart), fmtRFC(ap.BlockedEnd))
		}
		if !ap.Start.Equal(abs.Appts[0].Start) {
			t.Errorf("%s: formes de dates incohérentes", c.sunday)
		}
		// Samedi (sans plage) : un seul bloc de 24 h ; dimanche (08–12) : 00–08 et 12–minuit suivant.
		var sat, sun []service.AgendaClosed
		for _, cl := range wall.Closed {
			switch cl.Start.Format("2006-01-02") {
			case "2026-10-24", "2026-03-28":
				sat = append(sat, cl)
			case c.sunday:
				sun = append(sun, cl)
			}
		}
		if len(sat) != 1 || sat[0].End.Sub(sat[0].Start) != 24*time.Hour {
			t.Errorf("%s: samedi %+v", c.sunday, sat)
		}
		if len(sun) != 2 || sun[0].Start.Hour() != 0 || sun[0].End.Hour() != 8 || sun[1].Start.Hour() != 12 || sun[1].End.Format("2006-01-02T15:04") != c.nextMonday+"T00:00" {
			t.Errorf("%s: dimanche %+v", c.sunday, sun)
		}
		// Jour du changement d'heure : 25 h en octobre, 23 h en mars (durée réelle du jour).
		dayLen := sun[0].End.Sub(sun[0].Start) + 4*time.Hour + sun[1].End.Sub(sun[1].Start)
		want := 25 * time.Hour
		if c.sunday == "2026-03-29" {
			want = 23 * time.Hour
		}
		if dayLen != want {
			t.Errorf("%s: jour de %s, attendu %s", c.sunday, dayLen, want)
		}
	}
}

func TestAgendaRangeBoundsAndValidation(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	call := func(start, end string) error {
		_, err := f.cal.AgendaRange(ctx, f.act, service.EventsInput{Start: start, End: end})
		return err
	}
	// 42 jours calendaires à travers le changement d'heure d'octobre : accepté (42 × 24 h + 1 h).
	if err := call("2026-10-01T00:00:00", "2026-11-12T00:00:00"); err != nil {
		t.Errorf("42 jours: %v", err)
	}
	for name, c := range map[string][2]string{
		"43 jours":          {"2026-10-01", "2026-11-13T00:00:01"},
		"fin = début":       {"2026-10-19", "2026-10-19"},
		"fin avant début":   {"2026-10-20", "2026-10-19"},
		"début illisible":   {"hier", "2026-10-19"},
		"fin illisible":     {"2026-10-19", "demain"},
		"début vide":        {"", "2026-10-19"},
		"mois sans le jour": {"2026-10", "2026-11"},
	} {
		if err := call(c[0], c[1]); len(domain.FieldErrors(err)) == 0 {
			t.Errorf("%s : %v, attendu une erreur de validation", name, err)
		}
	}
}

func TestAgendaRangeExcludesCancelledAndOtherPractices(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	keep, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false))
	if err != nil {
		t.Fatal(err)
	}
	gone, err := f.cal.Create(ctx, f.act, service.NewAppointmentInput{PatientID: keep.PatientID.String(), TypeID: f.typeID.String(), Date: "2026-10-19", Time: "10:00"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.cal.Cancel(ctx, f.act, gone.ID, ""); err != nil {
		t.Fatal(err)
	}
	res := isoRange(t, f, "2026-10-19", "2026-10-20")
	if len(res.Appts) != 1 || res.Appts[0].ID != keep.ID || res.Appts[0].Patient != "Durand Léa" {
		t.Fatalf("RDV: %+v", res.Appts)
	}

	// Un autre cabinet ne voit rien du premier, et ne peut pas viser son praticien.
	u2, err := auth.CreateUser(ctx, f.db, auth.CreateUserParams{Email: "autre@example.test", Role: domain.RoleAdmin, PracticeName: "Autre", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	other := service.Actor{UserID: u2.ID, PracticeID: u2.PracticeID}
	res2, err := f.cal.AgendaRange(ctx, other, service.EventsInput{Start: "2026-10-19", End: "2026-10-20"})
	if err != nil || len(res2.Appts) != 0 {
		t.Errorf("autre cabinet: %v %+v", err, res2.Appts)
	}
	pr, err := f.svc.Practitioner(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.cal.AgendaRange(ctx, other, service.EventsInput{Start: "2026-10-19", End: "2026-10-20", Selection: service.Selection{IDs: []uuid.UUID{pr.ID}}})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("praticien d'un autre cabinet: %v", err)
	}
	// Un praticien ne vise pas l'agenda d'un confrère.
	mate := service.Actor{UserID: u2.ID, PracticeID: f.act.PracticeID, Role: domain.RolePractitioner, PractitionerID: uuid.New()}
	_, err = f.cal.AgendaRange(ctx, mate, service.EventsInput{Start: "2026-10-19", End: "2026-10-20", Selection: service.Selection{IDs: []uuid.UUID{pr.ID}}})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("confrère: %v", err)
	}
}

func TestAgendaDayListAndDST(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	// 2026-10-15 09:00 UTC = 11:00 à Paris.
	ag, err := f.cal.AgendaDay(ctx, f.act, "", service.Selection{})
	if err != nil || ag.Date != "2026-10-15" || ag.PrevDate != "2026-10-14" || ag.NextDate != "2026-10-16" || ag.TodayDate != "2026-10-15" {
		t.Fatalf("aujourd'hui: %v %+v", err, ag)
	}
	for _, c := range []struct{ date, prev, next string }{
		{"2026-10-25", "2026-10-24", "2026-10-26"},
		{"2026-03-29", "2026-03-28", "2026-03-30"},
	} {
		first, err := f.cal.Create(ctx, f.act, f.quick(c.date, "11:00", true))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.cal.Create(ctx, f.act, service.NewAppointmentInput{PatientID: first.PatientID.String(), TypeID: f.typeID.String(), Date: c.date, Time: "09:00", Force: true}); err != nil {
			t.Fatal(err)
		}
		ag, err := f.cal.AgendaDay(ctx, f.act, c.date, service.Selection{})
		if err != nil || ag.PrevDate != c.prev || ag.NextDate != c.next || len(ag.Appts) != 2 {
			t.Fatalf("%s: %v %+v", c.date, err, ag)
		}
		if ag.Appts[0].Start.Hour() != 9 || ag.Appts[1].Start.Hour() != 11 || ag.Appts[0].Practitioner == "" {
			t.Errorf("%s: tri ou heures: %+v", c.date, ag.Appts)
		}
	}
	if _, err := f.cal.AgendaDay(ctx, f.act, "pas-une-date", service.Selection{}); len(domain.FieldErrors(err)) == 0 {
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

func TestSearchPatientsAutocompleteForms(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	if _, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false)); err != nil {
		t.Fatal(err)
	}
	// prénom nom, nom prénom, sous-chaîne, casse, accents, téléphone partiel (forme
	// stockée E.164) et saisi en national.
	for _, q := range []string{"léa durand", "durand léa", "LEA DURAND", "urand", "lea", "+336123", "06 12 34 56 78", "0612345678"} {
		if ps, err := f.cal.SearchPatients(ctx, f.act, q); err != nil || len(ps) != 1 {
			t.Errorf("%q : %v %+v", q, err, ps)
		}
	}
}

func TestAuditAndLockAreScopedToPractice(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	ap, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.cal.Cancel(ctx, f.act, ap.ID, ""); err != nil {
		t.Fatal(err)
	}
	u, err := auth.CreateUser(ctx, f.db, auth.CreateUserParams{
		Email: "autre2@example.test", Role: domain.RoleAdmin, PracticeName: "Autre cabinet 2", Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	q := sqlcgen.New(f.db)
	entityID := pgtype.UUID{Bytes: ap.ID, Valid: true}
	own, err := q.ListAuditLogForEntity(ctx, sqlcgen.ListAuditLogForEntityParams{
		PracticeID: pgtype.UUID{Bytes: f.act.PracticeID, Valid: true}, Entity: "appointment", EntityID: entityID, PageSize: 50,
	})
	if err != nil || len(own) == 0 {
		t.Fatalf("journal du cabinet: %v %d", err, len(own))
	}
	other, err := q.ListAuditLogForEntity(ctx, sqlcgen.ListAuditLogForEntityParams{
		PracticeID: pgtype.UUID{Bytes: u.PracticeID, Valid: true}, Entity: "appointment", EntityID: entityID, PageSize: 50,
	})
	if err != nil || len(other) != 0 {
		t.Errorf("journal inter-cabinets: %v %d lignes", err, len(other))
	}
	// LockAppointment : lecture verrouillée dans une transaction.
	tx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qt := q.WithTx(tx)
	if _, err := qt.LockAppointment(ctx, sqlcgen.LockAppointmentParams{ID: ap.ID, PracticeID: u.PracticeID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("verrou inter-cabinets: %v", err)
	}
	if _, err := qt.LockAppointment(ctx, sqlcgen.LockAppointmentParams{ID: ap.ID, PracticeID: f.act.PracticeID}); err != nil {
		t.Errorf("verrou du cabinet: %v", err)
	}
}
