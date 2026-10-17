package web_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

type planEnv struct {
	c       *client
	db      *pgxpool.Pool
	actor   service.Actor
	patient sqlcgen.Patient
	typeID  string
}

func newPlanEnv(t *testing.T, role string) planEnv {
	t.Helper()
	db := testutil.DB(t)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: userEmail, Role: role, PracticeName: "Cabinet test", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	practice := &service.Practice{Pool: db, Clock: clock}
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: logger, DB: db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Practice: practice, Calendar: &service.Calendar{Pool: db, Clock: clock, Events: service.NopSink{}, ManageKey: service.NewManageKey(testutil.ManageSecret)},
		Patients: &service.Patients{Pool: db, Clock: clock}, CarePlans: &service.CarePlans{Pool: db, Clock: clock, Events: service.NopSink{}, ManageKey: service.NewManageKey(testutil.ManageSecret)},
	})
	c := newClient(t, h, "192.0.2.40")
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	actor := service.Actor{UserID: u.ID, PracticeID: u.PracticeID}
	ts, err := practice.AppointmentTypes(ctx, actor)
	if err != nil || len(ts) == 0 {
		t.Fatalf("motifs : %v", err)
	}
	if role != domain.RoleSecretary {
		var ids []string
		for _, ty := range ts {
			ids = append(ids, ty.ID.String())
		}
		if err := practice.AddRules(ctx, actor, service.RuleInput{Weekdays: []string{"0", "1", "2", "3", "4"}, Start: "08:00", End: "12:00", TypeIDs: ids}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := sqlcgen.New(db).CreatePatient(ctx, sqlcgen.CreatePatientParams{
		PracticeID: u.PracticeID, FirstName: "Paul", LastName: "Martin",
		Phone: pgtype.Text{String: "+33612345678", Valid: true}, SmsOptIn: true, EmailOptIn: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return planEnv{c: c, db: db, actor: actor, patient: p, typeID: ts[0].ID.String()}
}

func (e planEnv) criteria() url.Values {
	return url.Values{
		"type_id": {e.typeID}, "label": {"Rééducation"}, "prescribed": {"10"}, "per_week": {"2"},
		"start_date": {"2026-10-19"}, "weekdays": {"0", "3"},
	}
}

var (
	lineDateRe = regexp.MustCompile(`name="line_date" type="date" value="([0-9-]*)"`)
	lineTimeRe = regexp.MustCompile(`name="line_time" type="time" step="300" value="([0-9:]*)"`)
)

// linesOf lit les séances proposées dans la page rendue.
func linesOf(t *testing.T, body string) url.Values {
	t.Helper()
	ds, ts := lineDateRe.FindAllStringSubmatch(body, -1), lineTimeRe.FindAllStringSubmatch(body, -1)
	if len(ds) != len(ts) {
		t.Fatalf("%d dates pour %d heures", len(ds), len(ts))
	}
	v := url.Values{}
	for i := range ds {
		v.Add("line_date", ds[i][1])
		v.Add("line_time", ts[i][1])
	}
	return v
}

func TestCarePlanFlow(t *testing.T) {
	t.Parallel()
	e := newPlanEnv(t, domain.RoleAdmin)
	base := "/admin/patients/" + e.patient.ID.String()

	if body := e.c.do("GET", base+"/care-plans/new", nil).Body.String(); !strings.Contains(body, "Nouvelle série de séances") || !strings.Contains(body, "Proposer les séances") {
		t.Fatalf("formulaire : %s", body)
	}

	// Étape 1 : proposition automatique de 10 séances.
	rec := e.c.do("POST", base+"/care-plans/propose", e.criteria())
	if rec.Code != 200 {
		t.Fatalf("propose : %d", rec.Code)
	}
	lines := linesOf(t, rec.Body.String())
	if len(lines["line_date"]) != 10 || lines["line_date"][0] != "2026-10-19" || lines["line_date"][1] != "2026-10-22" {
		t.Fatalf("lignes proposées : %v", lines)
	}
	if !strings.Contains(rec.Body.String(), "Valider la série") {
		t.Error("bouton de validation absent")
	}

	// Critères invalides : 422 avec message.
	bad := e.criteria()
	bad.Set("prescribed", "0")
	if rec := e.c.do("POST", base+"/care-plans/propose", bad); rec.Code != 422 || !strings.Contains(rec.Body.String(), "entre 1 et 100") {
		t.Errorf("critères invalides : %d", rec.Code)
	}

	// Étape 2 : validation (le praticien décale la séance 3 à 09:00).
	form := e.criteria()
	for k, vs := range lines {
		form[k] = vs
	}
	form["line_time"][2] = "09:00"
	rec = e.c.do("POST", base+"/care-plans", form)
	if rec.Code != 303 || rec.Header().Get("Location") != base {
		t.Fatalf("create : %d %s", rec.Code, rec.Header().Get("Location"))
	}
	page := e.c.do("GET", base, nil).Body.String()
	for _, want := range []string{"Rééducation", "10/10 séances", "séance 3/10", "09h00"} {
		if !strings.Contains(page, want) {
			t.Errorf("fiche patient sans %q", want)
		}
	}

	// Badge sur le détail d'un RDV de la série, puis annulation et remplacement.
	m := apptHref.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("aucun rendez-vous sur la fiche")
	}
	apptURL := "/admin/appointments/" + m[1]
	if body := e.c.do("GET", apptURL, nil).Body.String(); !strings.Contains(body, "Séance 1/10") {
		t.Errorf("badge absent : %s", body)
	}
	if rec := e.c.do("POST", apptURL+"/cancel", url.Values{"date": {"2026-10-19"}, "view": {"week"}}); rec.Code != 303 {
		t.Fatalf("annulation : %d", rec.Code)
	}
	body := e.c.do("GET", apptURL, nil).Body.String()
	if !strings.Contains(body, "Remplacer la séance annulée") || !strings.Contains(body, "Planifier cette séance") {
		t.Fatalf("remplacement non proposé : %s", body)
	}
	planID := regexp.MustCompile(`/admin/care-plans/([0-9a-f-]{36})/replace`).FindStringSubmatch(body)
	dm := regexp.MustCompile(`name="date" value="([0-9-]+)"`).FindStringSubmatch(body)
	tm := regexp.MustCompile(`name="time" value="([0-9:]+)"`).FindStringSubmatch(body)
	if planID == nil || dm == nil || tm == nil {
		t.Fatal("formulaire de remplacement incomplet")
	}
	rec = e.c.do("POST", "/admin/care-plans/"+planID[1]+"/replace", url.Values{"appointment_id": {m[1]}, "date": {dm[1]}, "time": {tm[1]}})
	if rec.Code != 303 {
		t.Fatalf("remplacement : %d", rec.Code)
	}
	if page := e.c.do("GET", base, nil).Body.String(); !strings.Contains(page, "10/10 séances") {
		t.Error("le décompte devrait être revenu à 10/10")
	}
}

func TestCarePlanConflictRendersAllOrNothing(t *testing.T) {
	t.Parallel()
	e := newPlanEnv(t, domain.RoleAdmin)
	base := "/admin/patients/" + e.patient.ID.String()
	// Un RDV existant occupe lundi 26/10 à 08:00.
	if _, err := (&service.Calendar{Pool: e.db, Clock: testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)), ManageKey: service.NewManageKey(testutil.ManageSecret)}).Create(context.Background(), e.actor, service.NewAppointmentInput{
		PatientID: e.patient.ID.String(), TypeID: e.typeID, Date: "2026-10-26", Time: "08:00",
	}); err != nil {
		t.Fatal(err)
	}
	form := e.criteria()
	form["line_date"] = []string{"2026-10-19", "2026-10-22", "2026-10-26"}
	form["line_time"] = []string{"08:00", "08:00", "08:00"}
	rec := e.c.do("POST", base+"/care-plans", form)
	body := rec.Body.String()
	if rec.Code != 422 || !strings.Contains(body, "Séance 3") || !strings.Contains(body, "Aucune séance n&#39;a été créée") || !strings.Contains(body, `aria-invalid="true"`) {
		t.Fatalf("conflit : %d %s", rec.Code, body)
	}
	// Les valeurs saisies sont conservées pour l'ajustement.
	if got := linesOf(t, body); fmt.Sprint(got["line_date"]) != "[2026-10-19 2026-10-22 2026-10-26]" {
		t.Errorf("lignes conservées : %v", got)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), "SELECT count(*) FROM appointment WHERE care_plan_id IS NOT NULL").Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rendez-vous de série créés malgré le conflit (%v)", n, err)
	}
}

// secretaryPlanEnv : un cabinet avec un admin (dont le profil praticien porte les
// horaires), un secrétaire connecté, un patient, et un praticien d'un autre cabinet.
type secretaryPlanEnv struct {
	c        *client // secrétaire
	db       *pgxpool.Pool
	patient  sqlcgen.Patient
	typeID   string
	pracID   uuid.UUID
	foreign  uuid.UUID
	pracName string
}

func newSecretaryPlanEnv(t *testing.T) secretaryPlanEnv {
	t.Helper()
	db := testutil.DB(t)
	ctx := context.Background()
	adm, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: "admin@example.test", Role: domain.RoleAdmin, PracticeName: "Cabinet test", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: userEmail, Role: domain.RoleSecretary, PracticeName: "Cabinet test", Password: pw}); err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	practice := &service.Practice{Pool: db, Clock: clock}
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: logger, DB: db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Practice: practice, Calendar: &service.Calendar{Pool: db, Clock: clock, Events: service.NopSink{}, ManageKey: service.NewManageKey(testutil.ManageSecret)},
		Patients: &service.Patients{Pool: db, Clock: clock}, CarePlans: &service.CarePlans{Pool: db, Clock: clock, Events: service.NopSink{}, ManageKey: service.NewManageKey(testutil.ManageSecret)},
	})
	c := newClient(t, h, "192.0.2.41")
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	actor := service.Actor{UserID: adm.ID, PracticeID: adm.PracticeID}
	ts, err := practice.AppointmentTypes(ctx, actor)
	if err != nil || len(ts) == 0 {
		t.Fatalf("motifs : %v", err)
	}
	var ids []string
	for _, ty := range ts {
		ids = append(ids, ty.ID.String())
	}
	if err := practice.AddRules(ctx, actor, service.RuleInput{Weekdays: []string{"0", "1", "2", "3", "4"}, Start: "08:00", End: "12:00", TypeIDs: ids}); err != nil {
		t.Fatal(err)
	}
	pr, err := practice.Practitioner(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	p, err := sqlcgen.New(db).CreatePatient(ctx, sqlcgen.CreatePatientParams{
		PracticeID: adm.PracticeID, FirstName: "Paul", LastName: "Martin",
		Phone: pgtype.Text{String: "+33612345678", Valid: true}, SmsOptIn: true, EmailOptIn: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	other := testutil.NewPractice(t, db)
	return secretaryPlanEnv{
		c: c, db: db, patient: p, typeID: ts[0].ID.String(), pracID: pr.ID, pracName: service.PractitionerName(pr),
		foreign: testutil.NewPractitioner(t, db, other.ID).ID,
	}
}

func (e secretaryPlanEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Le secrétariat crée, ajuste et change le statut d'une série, en choisissant le
// praticien visé ; il ne peut pas viser un praticien d'un autre cabinet.
func TestCarePlanSecretaryFlow(t *testing.T) {
	t.Parallel()
	e := newSecretaryPlanEnv(t)
	base := "/admin/patients/" + e.patient.ID.String()
	criteria := func(practitioner string) url.Values {
		v := url.Values{
			"type_id": {e.typeID}, "label": {"Rééducation"}, "prescribed": {"10"}, "per_week": {"2"},
			"start_date": {"2026-10-19"}, "weekdays": {"0", "3"},
		}
		if practitioner != "" {
			v.Set("practitioner", practitioner)
		}
		return v
	}

	// Le formulaire propose le choix du praticien.
	mustContain(t, e.c.do("GET", base+"/care-plans/new", nil), 200, `name="practitioner"`, e.pracName, "Choisir…")
	// Sans praticien : message de validation, rien d'écrit.
	rec := e.c.do("POST", base+"/care-plans/propose", criteria(""))
	mustContain(t, rec, 422, "Choisissez le praticien des séances.")

	// Proposition puis ajustement d'une séance (09:00) et validation.
	rec = e.c.do("POST", base+"/care-plans/propose", criteria(e.pracID.String()))
	mustContain(t, rec, 200, "Valider la série", `<option value="`+e.pracID.String()+`" selected>`)
	lines := linesOf(t, rec.Body.String())
	if len(lines["line_date"]) != 10 {
		t.Fatalf("lignes proposées : %v", lines)
	}
	form := criteria(e.pracID.String())
	for k, vs := range lines {
		form[k] = vs
	}
	form["line_time"][2] = "09:00"
	if rec := e.c.do("POST", base+"/care-plans", form); rec.Code != 303 || rec.Header().Get("Location") != base {
		t.Fatalf("create : %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if n := e.count(t, "SELECT count(*) FROM care_plan WHERE practitioner_id = $1", e.pracID); n != 1 {
		t.Fatalf("%d série(s) pour le praticien choisi", n)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE care_plan_id IS NOT NULL AND practitioner_id = $1", e.pracID); n != 10 {
		t.Errorf("%d séances planifiées, attendu 10", n)
	}
	page := e.c.do("GET", base, nil).Body.String()
	for _, want := range []string{"Rééducation", "10/10 séances", "09h00"} {
		if !strings.Contains(page, want) {
			t.Errorf("fiche patient sans %q", want)
		}
	}

	// Changement de statut par le secrétariat.
	m := regexp.MustCompile(`/admin/care-plans/([0-9a-f-]{36})/status`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("formulaire de statut absent")
	}
	for _, status := range []string{"completed", "active"} {
		if rec := e.c.do("POST", "/admin/care-plans/"+m[1]+"/status", url.Values{"status": {status}}); rec.Code != 303 {
			t.Fatalf("statut %s : %d", status, rec.Code)
		}
		if n := e.count(t, "SELECT count(*) FROM care_plan WHERE id = $1 AND status = $2", m[1], status); n != 1 {
			t.Errorf("statut %s non enregistré", status)
		}
	}
}

func TestCarePlanSecretaryCannotTargetOtherPractice(t *testing.T) {
	t.Parallel()
	e := newSecretaryPlanEnv(t)
	base := "/admin/patients/" + e.patient.ID.String()
	form := url.Values{
		"type_id": {e.typeID}, "prescribed": {"2"}, "per_week": {"2"}, "start_date": {"2026-10-19"},
		"practitioner": {e.foreign.String()}, "line_date": {"2026-10-19", "2026-10-22"}, "line_time": {"08:00", "08:00"},
	}
	for _, p := range []string{base + "/care-plans/propose", base + "/care-plans"} {
		if rec := e.c.do("POST", p, form); rec.Code != 404 {
			t.Errorf("POST %s vers un praticien d'un autre cabinet : %d, attendu 404", p, rec.Code)
		}
	}
	if rec := e.c.do("POST", base+"/care-plans/propose", func() url.Values { form.Set("practitioner", "pas-un-uuid"); return form }()); rec.Code != 404 {
		t.Errorf("identifiant invalide : %d, attendu 404", rec.Code)
	}
	if n := e.count(t, "SELECT count(*) FROM care_plan"); n != 0 {
		t.Errorf("%d série(s) créée(s)", n)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE care_plan_id IS NOT NULL"); n != 0 {
		t.Errorf("%d séance(s) créée(s)", n)
	}
}
