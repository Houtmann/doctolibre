package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

const testPassword = "un-mot-de-passe-solide" //nolint:gosec // mot de passe de test

type fixture struct {
	db  *pgxpool.Pool
	svc *service.Practice
	act service.Actor
}

// newFixture crée un cabinet + un utilisateur (via auth.CreateUser, donc avec les
// motifs par défaut) ; l'horloge est figée au 2026-10-15 09:00 UTC.
func newFixture(t *testing.T) fixture {
	t.Helper()
	db := testutil.DB(t)
	u, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{
		Email: "kine@example.test", Role: domain.RoleAdmin, PracticeName: "Cabinet test", Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	return fixture{
		db:  db,
		svc: &service.Practice{Pool: db, Clock: clock},
		act: service.Actor{UserID: u.ID, PracticeID: u.PracticeID},
	}
}

func messages(err error) string {
	var out []string
	for _, e := range domain.FieldErrors(err) {
		out = append(out, e.Msg)
	}
	return strings.Join(out, " | ")
}

func TestSeedAppointmentTypesIdempotent(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t)
	p := testutil.NewPractice(t, db)
	q := sqlcgen.New(db)
	ctx := context.Background()
	for range 2 {
		if err := service.SeedAppointmentTypes(ctx, q, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	ts, err := q.ListAllAppointmentTypes(ctx, sqlcgen.ListAllAppointmentTypesParams{PracticeID: p.ID, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 3 {
		t.Fatalf("motifs: %d, attendu 3", len(ts))
	}
	want := []struct {
		name         string
		min          int
		online, home bool
	}{{"Bilan initial", 45, true, false}, {"Séance", 30, true, false}, {"Séance à domicile", 30, false, true}}
	for i, w := range want {
		d, _, _ := service.TypeMinutes(ts[i])
		if ts[i].Name != w.name || d != w.min || ts[i].OnlineBookable != w.online || ts[i].AtHome != w.home {
			t.Errorf("motif %d: %+v", i, ts[i])
		}
	}
}

func TestCreateUserSeedsNewPracticeOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ts, err := f.svc.AppointmentTypes(context.Background(), f.act)
	if err != nil || len(ts) != 3 {
		t.Fatalf("motifs du cabinet neuf: %d, %v", len(ts), err)
	}
	// Second utilisateur dans le même cabinet : pas de doublon.
	if _, err := auth.CreateUser(context.Background(), f.db, auth.CreateUserParams{
		Email: "secretaire@example.test", Role: domain.RoleSecretary, PracticeName: "Cabinet test", Password: testPassword,
	}); err != nil {
		t.Fatal(err)
	}
	if ts, _ = f.svc.AppointmentTypes(context.Background(), f.act); len(ts) != 3 {
		t.Fatalf("motifs après 2e utilisateur: %d", len(ts))
	}
}

func TestPractitionerCreatedOnceAndSecretaryForbidden(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	p1, err := f.svc.Practitioner(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := f.svc.Practitioner(ctx, f.act)
	if err != nil || p1.ID != p2.ID {
		t.Fatalf("praticien non stable: %v %v %v", p1.ID, p2.ID, err)
	}
	sec, err := auth.CreateUser(ctx, f.db, auth.CreateUserParams{
		Email: "secretaire@example.test", Role: domain.RoleSecretary, PracticeName: "Cabinet test", Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Practitioner(ctx, service.Actor{UserID: sec.ID, PracticeID: sec.PracticeID}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("secrétaire: %v", err)
	}
	// Autre cabinet : utilisateur introuvable.
	other := testutil.NewPractice(t, f.db)
	if _, err := f.svc.Practitioner(ctx, service.Actor{UserID: f.act.UserID, PracticeID: other.ID}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("autre cabinet: %v", err)
	}
}

func validSettings() service.SettingsInput {
	return service.SettingsInput{
		Name: "Cabinet Étoile", Address: "1 rue de la Paix, Paris", Phone: "01 23 45 67 89",
		BookingNoticeHours: "4", CancelNoticeHours: "48", HorizonDays: "90",
		FirstName: "Camille", LastName: "Martin", Title: "Masseur-kinésithérapeute", Bio: "Bio", Slug: "camille-martin", Color: "#aa3366",
	}
}

func TestUpdateSettings(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		mutate func(*service.SettingsInput)
		want   string // fragment attendu dans les messages ; "" = valide
	}{
		{"valide", func(*service.SettingsInput) {}, ""},
		{"nom vide", func(in *service.SettingsInput) { in.Name = "  " }, "nom du cabinet est requis"},
		{"délai non numérique", func(in *service.SettingsInput) { in.BookingNoticeHours = "abc" }, "délai minimal de réservation"},
		{"délai négatif", func(in *service.SettingsInput) { in.CancelNoticeHours = "-1" }, "délai minimal d'annulation"},
		{"horizon nul", func(in *service.SettingsInput) { in.HorizonDays = "0" }, "horizon"},
		{"téléphone invalide", func(in *service.SettingsInput) { in.Phone = "abc" }, "téléphone"},
		{"slug invalide", func(in *service.SettingsInput) { in.Slug = "Pas Valide" }, "identifiant d'URL"},
		{"prénom vide", func(in *service.SettingsInput) { in.FirstName = "" }, "prénom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validSettings()
			tt.mutate(&in)
			err := f.svc.UpdateSettings(ctx, f.act, in)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("erreur inattendue: %v", err)
				}
				return
			}
			if !strings.Contains(messages(err), tt.want) {
				t.Fatalf("messages %q, attendu %q (err=%v)", messages(err), tt.want, err)
			}
		})
	}

	// Les valeurs valides ont été enregistrées (le dernier cas valide est le 1er).
	s, err := f.svc.Settings(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	if s.Practice.Name != "Cabinet Étoile" || s.BookingNoticeHour != 4 || s.CancelNoticeHour != 48 || s.HorizonDays != 90 ||
		s.Practitioner.Slug != "camille-martin" || s.Practitioner.FirstName != "Camille" {
		t.Errorf("paramètres relus: %+v", s)
	}
}

func TestUpdateSettingsSlugTaken(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	testutil.NewPractitioner(t, f.db, f.act.PracticeID) // slug "praticien-<uuid>"
	other, err := sqlcgen.New(f.db).ListPractitioners(ctx, sqlcgen.ListPractitionersParams{PracticeID: f.act.PracticeID, PageSize: 10})
	if err != nil || len(other) == 0 {
		t.Fatal(err)
	}
	in := validSettings()
	in.Slug = other[0].Slug
	err = f.svc.UpdateSettings(ctx, f.act, in)
	if !strings.Contains(messages(err), "déjà utilisé") {
		t.Fatalf("slug en doublon: %v", err)
	}
}

func TestAppointmentTypeValidationAndOrder(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()

	bad := []service.AppointmentTypeInput{
		{Name: "", DurationMin: "30"},
		{Name: "x", DurationMin: "0"},
		{Name: "x", DurationMin: "abc"},
		{Name: "x", DurationMin: "30", BufferAfterMin: "-5"},
		{Name: "x", DurationMin: "30", Color: "rouge"},
	}
	for _, in := range bad {
		if _, err := f.svc.CreateAppointmentType(ctx, f.act, in); len(domain.FieldErrors(err)) == 0 {
			t.Errorf("%+v accepté (err=%v)", in, err)
		}
	}

	created, err := f.svc.CreateAppointmentType(ctx, f.act, service.AppointmentTypeInput{
		Name: "Drainage", DurationMin: "60", BufferAfterMin: "10", OnlineBookable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := f.svc.AppointmentTypes(ctx, f.act)
	if len(ts) != 4 || ts[3].ID != created.ID {
		t.Fatalf("le nouveau motif doit être dernier: %+v", ts)
	}
	if d, _, a := service.TypeMinutes(ts[3]); d != 60 || a != 10 {
		t.Errorf("durées: %d %d", d, a)
	}

	// Monter le dernier : il passe 3e ; positions contiguës.
	if err := f.svc.MoveAppointmentType(ctx, f.act, created.ID, -1); err != nil {
		t.Fatal(err)
	}
	ts, _ = f.svc.AppointmentTypes(ctx, f.act)
	if ts[2].ID != created.ID {
		t.Errorf("ordre après montée: %s en position 3 ?", ts[2].Name)
	}
	for i, ty := range ts {
		if int(ty.Position) != i {
			t.Errorf("position %d = %d", i, ty.Position)
		}
	}

	// Désactivation : le motif reste listé mais inactif.
	if err := f.svc.SetAppointmentTypeActive(ctx, f.act, created.ID, false); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.AppointmentType(ctx, f.act, created.ID)
	if err != nil || got.Active {
		t.Fatalf("désactivation: %+v %v", got, err)
	}

	// Un motif d'un autre cabinet est introuvable.
	other := testutil.NewPractice(t, f.db)
	if _, err := f.svc.AppointmentType(ctx, service.Actor{UserID: f.act.UserID, PracticeID: other.ID}, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("autre cabinet: %v", err)
	}
	if err := f.svc.UpdateAppointmentType(ctx, service.Actor{UserID: f.act.UserID, PracticeID: other.ID}, created.ID,
		service.AppointmentTypeInput{Name: "x", DurationMin: "30"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("mise à jour autre cabinet: %v", err)
	}
}

func TestAddRulesOverlap(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	rule := func(days []string, start, end string) service.RuleInput {
		return service.RuleInput{Weekdays: days, Start: start, End: end, TypeIDs: allTypeIDs(t, f)}
	}

	if err := f.svc.AddRules(ctx, f.act, rule([]string{"0", "1"}, "08:00", "12:00")); err != nil {
		t.Fatalf("plage valide: %v", err)
	}
	tests := []struct {
		name  string
		in    service.RuleInput
		want  string // "" = accepté
		count int    // plages totales attendues ensuite
	}{
		{"chevauchement partiel", rule([]string{"0"}, "10:00", "13:00"), "Lundi", 2},
		{"inclus", rule([]string{"0"}, "09:00", "10:00"), "chevauche", 2},
		{"englobant", rule([]string{"1"}, "07:00", "18:00"), "chevauche", 2},
		{"tout ou rien sur plusieurs jours", rule([]string{"2", "0"}, "11:00", "12:30"), "chevauche", 2},
		{"contigu accepté", rule([]string{"0"}, "12:00", "14:00"), "", 3},
		{"autre jour accepté", rule([]string{"2"}, "08:00", "12:00"), "", 4},
		{"fin avant début", rule([]string{"3"}, "12:00", "08:00"), "après l'heure de début", 4},
		{"fin = début", rule([]string{"3"}, "08:00", "08:00"), "après l'heure de début", 4},
		{"format invalide", rule([]string{"3"}, "8h", "12:00"), "HH:MM", 4},
		{"aucun jour", rule(nil, "08:00", "12:00"), "au moins un jour", 4},
		{"jour invalide", rule([]string{"7"}, "08:00", "12:00"), "Jour", 4},
		{"aucun motif", service.RuleInput{Weekdays: []string{"4"}, Start: "08:00", End: "12:00"}, "au moins un motif", 4},
	}
	for _, tt := range tests {
		err := f.svc.AddRules(ctx, f.act, tt.in)
		if tt.want == "" {
			if err != nil {
				t.Errorf("%s: erreur inattendue %v", tt.name, err)
			}
		} else if !strings.Contains(messages(err), tt.want) {
			t.Errorf("%s: messages %q, attendu %q", tt.name, messages(err), tt.want)
		}
		if n := countRules(t, f); n != tt.count {
			t.Errorf("%s: %d plages, attendu %d", tt.name, n, tt.count)
		}
	}

	// Suppression d'une plage.
	v, err := f.svc.Availability(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Days[0].Rules; len(got) != 2 || got[0].Start != "08:00" || got[1].End != "14:00" || !got[0].AllTypes {
		t.Fatalf("lundi: %+v", got)
	}
	if err := f.svc.DeleteRule(ctx, f.act, v.Days[0].Rules[0].ID); err != nil {
		t.Fatal(err)
	}
	if n := countRules(t, f); n != 3 {
		t.Errorf("après suppression: %d", n)
	}
}

func allTypeIDs(t *testing.T, f fixture) []string {
	t.Helper()
	ts, err := f.svc.AppointmentTypes(context.Background(), f.act)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, ty := range ts {
		ids = append(ids, ty.ID.String())
	}
	return ids
}

func countRules(t *testing.T, f fixture) int {
	t.Helper()
	v, err := f.svc.Availability(context.Background(), f.act)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range v.Days {
		n += len(d.Rules)
	}
	return n
}

func TestAddRulesTypeRestriction(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	ids := allTypeIDs(t, f)
	if err := f.svc.AddRules(ctx, f.act, service.RuleInput{Weekdays: []string{"0"}, Start: "08:00", End: "12:00", TypeIDs: ids[:1]}); err != nil {
		t.Fatal(err)
	}
	v, _ := f.svc.Availability(ctx, f.act)
	r := v.Days[0].Rules[0]
	if r.AllTypes || len(r.TypeNames) != 1 || r.TypeNames[0] != "Bilan initial" {
		t.Errorf("restriction: %+v", r)
	}
	// Motif inconnu.
	err := f.svc.AddRules(ctx, f.act, service.RuleInput{Weekdays: []string{"1"}, Start: "08:00", End: "12:00", TypeIDs: []string{uuid.NewString()}})
	if !strings.Contains(messages(err), "inconnu") {
		t.Errorf("motif inconnu: %v", err)
	}
}

// L'invariant tient aussi en base, hors du service (course entre deux requêtes).
func TestAvailabilityRuleOverlapConstraint(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	pr, err := f.svc.Practitioner(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	q := sqlcgen.New(f.db)
	mk := func(startH, endH int, from, until string) error {
		p := sqlcgen.CreateAvailabilityRuleParams{
			PractitionerID: pr.ID, Weekday: 2,
			StartTime: pgtype.Time{Microseconds: int64(startH) * 3600e6, Valid: true},
			EndTime:   pgtype.Time{Microseconds: int64(endH) * 3600e6, Valid: true},
			ValidFrom: date(t, from),
		}
		if until != "" {
			p.ValidUntil = date(t, until)
		}
		_, err := q.CreateAvailabilityRule(ctx, p)
		return err
	}
	if err := mk(8, 12, "2026-01-01", "2026-06-30"); err != nil {
		t.Fatal(err)
	}
	if err := mk(10, 14, "2026-06-30", ""); !store.IsExclusionViolation(err) {
		t.Errorf("chevauchement (dernier jour inclus) accepté: %v", err)
	}
	if err := mk(10, 14, "2026-07-01", ""); err != nil {
		t.Errorf("périodes disjointes refusées: %v", err)
	}
	if err := mk(12, 14, "2026-01-01", "2026-06-30"); err != nil {
		t.Errorf("plages contiguës refusées: %v", err)
	}
}

func date(t *testing.T, s string) pgtype.Date {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return pgtype.Date{Time: d, Valid: true}
}

func TestClosures(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()

	bad := []struct {
		name string
		in   service.ClosureInput
		want string
	}{
		{"sans date", service.ClosureInput{}, "date de début"},
		{"une seule heure", service.ClosureInput{StartDate: "2026-11-02", StartTime: "08:00"}, "deux heures"},
		{"fin avant début", service.ClosureInput{StartDate: "2026-11-05", EndDate: "2026-11-03"}, "après son début"},
		{"heures inversées", service.ClosureInput{StartDate: "2026-11-05", StartTime: "12:00", EndTime: "09:00"}, "après son début"},
		{"passé", service.ClosureInput{StartDate: "2026-10-01", EndDate: "2026-10-02"}, "futur"},
		{"heure invalide", service.ClosureInput{StartDate: "2026-11-05", StartTime: "25:00", EndTime: "26:00"}, "HH:MM"},
	}
	for _, tt := range bad {
		if err := f.svc.AddClosure(ctx, f.act, tt.in); !strings.Contains(messages(err), tt.want) {
			t.Errorf("%s: %q, attendu %q", tt.name, messages(err), tt.want)
		}
	}

	// Journées entières sur le passage à l'heure d'hiver (dimanche 25 octobre 2026 : 25 h).
	if err := f.svc.AddClosure(ctx, f.act, service.ClosureInput{StartDate: "2026-10-25", Reason: "Formation"}); err != nil {
		t.Fatal(err)
	}
	// Plage horaire sur plusieurs jours.
	if err := f.svc.AddClosure(ctx, f.act, service.ClosureInput{StartDate: "2026-11-02", StartTime: "14:00", EndDate: "2026-11-03", EndTime: "10:30"}); err != nil {
		t.Fatal(err)
	}
	cs, err := f.svc.Closures(ctx, f.act)
	if err != nil || len(cs) != 2 {
		t.Fatalf("absences: %d %v", len(cs), err)
	}
	dst := cs[0]
	if !dst.AllDay || dst.Reason != "Formation" {
		t.Errorf("journée entière: %+v", dst)
	}
	if got := dst.End.Sub(dst.Start); got != 25*time.Hour {
		t.Errorf("durée du jour de changement d'heure: %v, attendu 25h", got)
	}
	if !dst.End.UTC().Equal(time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("fin en UTC: %v", dst.End.UTC())
	}
	if cs[1].AllDay || cs[1].Start.Hour() != 14 || cs[1].End.Minute() != 30 {
		t.Errorf("plage horaire: %+v", cs[1])
	}

	// Une absence terminée n'est plus listée ; suppression.
	if err := f.svc.DeleteClosure(ctx, f.act, dst.ID); err != nil {
		t.Fatal(err)
	}
	if cs, _ = f.svc.Closures(ctx, f.act); len(cs) != 1 {
		t.Errorf("après suppression: %d", len(cs))
	}
}

func TestClosureSpringForward(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	f.svc.Clock = testutil.NewFixedClock(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC))
	// Dimanche 29 mars 2026 : passage à l'heure d'été, journée de 23 h.
	if err := f.svc.AddClosure(ctx, f.act, service.ClosureInput{StartDate: "2026-03-29"}); err != nil {
		t.Fatal(err)
	}
	cs, err := f.svc.Closures(ctx, f.act)
	if err != nil || len(cs) != 1 {
		t.Fatalf("%d %v", len(cs), err)
	}
	if got := cs[0].End.Sub(cs[0].Start); got != 23*time.Hour {
		t.Errorf("durée: %v, attendu 23h", got)
	}
}

func TestUpdateSettingsColor(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	read := func() (name, slug, color string) {
		t.Helper()
		pr, err := f.svc.Practitioner(ctx, f.act)
		if err != nil {
			t.Fatal(err)
		}
		pc, err := sqlcgen.New(f.db).GetPractice(ctx, f.act.PracticeID)
		if err != nil {
			t.Fatal(err)
		}
		return pc.Name, pr.Slug, pr.Color
	}
	in := validSettings()
	in.Color = " #aa3366 "
	if err := f.svc.UpdateSettings(ctx, f.act, in); err != nil {
		t.Fatal(err)
	}
	name, slug, color := read()
	if color != "#aa3366" {
		t.Fatalf("couleur = %q", color)
	}
	for _, bad := range []string{"rouge", "#12345", "url(x)", ""} {
		in := validSettings()
		in.Name, in.Slug, in.Color = "Autre nom", "autre-slug", bad
		err := f.svc.UpdateSettings(ctx, f.act, in)
		fe := domain.FieldErrors(err)
		if len(fe) != 1 || fe[0].Field != "color" {
			t.Errorf("couleur %q: %v", bad, err)
		}
		if n, s, c := read(); n != name || s != slug || c != color {
			t.Errorf("couleur %q: écriture partielle (%q, %q, %q)", bad, n, s, c)
		}
	}
}
