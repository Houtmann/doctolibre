package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

type calEnv struct {
	h      http.Handler
	c      *client
	db     *pgxpool.Pool
	cal    *service.Calendar
	actor  service.Actor
	typeID uuid.UUID
}

// newCalendarEnv monte le handler complet avec l'agenda ; l'horloge est figée au
// jeudi 15 octobre 2026 11:00 (Paris). Le praticien est ouvert lundi–vendredi 08–12.
func newCalendarEnv(t *testing.T, role string) calEnv {
	t.Helper()
	db := testutil.DB(t)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: userEmail, Role: role, PracticeName: "Cabinet test", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cal := &service.Calendar{Pool: db, Clock: clock, Events: service.NopSink{}}
	practice := &service.Practice{Pool: db, Clock: clock}
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: logger, DB: db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Practice: practice, Calendar: cal, Feed: &service.CalendarFeed{Pool: db, Clock: clock},
	})
	c := newClient(t, h, "192.0.2.20")
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	actor := service.Actor{UserID: u.ID, PracticeID: u.PracticeID}
	if role != domain.RoleSecretary {
		ts, err := cal.ActiveTypes(ctx, actor)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, ty := range ts {
			ids = append(ids, ty.ID.String())
		}
		if err := practice.AddRules(ctx, actor, service.RuleInput{Weekdays: []string{"0", "1", "2", "3", "4"}, Start: "08:00", End: "12:00", TypeIDs: ids}); err != nil {
			t.Fatal(err)
		}
	}
	ty, err := sqlcgen.New(db).CreateAppointmentType(ctx, sqlcgen.CreateAppointmentTypeParams{
		PracticeID: u.PracticeID, Name: "Séance 30",
		Duration:     pgtype.Interval{Microseconds: (30 * time.Minute).Microseconds(), Valid: true},
		BufferBefore: pgtype.Interval{Valid: true},
		BufferAfter:  pgtype.Interval{Microseconds: (10 * time.Minute).Microseconds(), Valid: true},
		Color:        "#2563eb",
	})
	if err != nil {
		t.Fatal(err)
	}
	return calEnv{h: h, c: c, db: db, cal: cal, actor: actor, typeID: ty.ID}
}

func (e calEnv) form(date, clock string) url.Values {
	return url.Values{
		"new_last_name": {"Durand"}, "new_first_name": {"Léa"}, "new_phone": {"0612345678"},
		"type_id": {e.typeID.String()}, "date": {date}, "time": {clock}, "view": {"week"},
	}
}

var apptHref = regexp.MustCompile(`href="/admin/appointments/([0-9a-f-]{36})"`)

func (e calEnv) firstAppointmentID(t *testing.T, date string) string {
	t.Helper()
	rec := e.c.do("GET", "/admin/calendar?view=week&date="+date, nil)
	m := apptHref.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("aucun rendez-vous dans l'agenda du %s", date)
	}
	return m[1]
}

func TestCalendarAccess(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	anon := newClient(t, e.h, "192.0.2.21")
	for _, p := range []string{"/admin/calendar", "/admin/appointments/new", "/admin/patients/search?q=du"} {
		if rec := anon.do("GET", p, nil); rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/login") {
			t.Errorf("GET %s sans session: %d", p, rec.Code)
		}
	}
	if rec := anon.do("POST", "/admin/appointments", url.Values{}); rec.Code != 303 {
		t.Errorf("POST sans session: %d", rec.Code)
	}
	if rec := e.c.do("GET", "/admin/appointments/not-a-uuid", nil); rec.Code != 404 {
		t.Errorf("id invalide: %d", rec.Code)
	}
	if rec := e.c.do("GET", "/admin/appointments/"+uuid.NewString(), nil); rec.Code != 404 {
		t.Errorf("RDV inconnu: %d", rec.Code)
	}
	sec := newCalendarEnv(t, domain.RoleSecretary)
	// Le secrétariat voit les agendas (T033) mais pas les réglages.
	if rec := sec.c.do("GET", "/admin/calendar", nil); rec.Code != 200 {
		t.Errorf("secrétaire, agenda: %d", rec.Code)
	}
	if rec := sec.c.do("GET", "/admin/settings", nil); rec.Code != 403 {
		t.Errorf("secrétaire, réglages: %d", rec.Code)
	}
}

func TestCalendarPostRequiresCSRF(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	id := uuid.NewString()
	for _, p := range []string{"/admin/appointments", "/admin/appointments/" + id + "/move", "/admin/appointments/" + id + "/cancel", "/admin/appointments/" + id + "/status"} {
		rec := e.c.doNoCSRF("POST", p)
		if rec.Code != 403 {
			t.Errorf("POST %s sans CSRF: %d", p, rec.Code)
		}
	}
}

func TestCalendarPageWeekAndDay(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	if rec := e.c.do("POST", "/admin/appointments", e.form("2026-10-19", "09:00")); rec.Code != 303 {
		t.Fatalf("création: %d %s", rec.Code, rec.Body.String())
	}
	rec := e.c.do("GET", "/admin/calendar?view=week&date=2026-10-21", nil)
	mustContain(t, rec, 200,
		`id="calendar"`, `hx-trigger="every 60s"`, `cal-cols-7`, `Semaine du lundi 19 octobre au dimanche 25 octobre`,
		// 09:00 → ligne (9-7)×4+2 = 10, 30 min → 2 lignes, colonne du lundi = 2.
		`class="cal-appt gr-10 gs-2 gc-2 st-booked"`, `Durand Léa`, `cal-band`, `<dialog id="appt-dialog"`,
		`href="/admin/calendar?date=2026-10-14&amp;view=week"`, `href="/admin/calendar?date=2026-10-28&amp;view=week"`,
		`class="cal-list"`,
	)
	if strings.Contains(rec.Body.String(), `<div class="cal-now`) {
		t.Errorf("ligne « maintenant » hors de la vue du jour")
	}
	// htmx : le fragment de la grille seul.
	rec = e.c.doHXGet("/admin/calendar?view=day&date=2026-10-15")
	mustContain(t, rec, 200, `id="calendar"`, `class="cal-now gr-18 gc-2"`, `jeudi 15 octobre`)
	if strings.Contains(rec.Body.String(), "<html") || strings.Contains(rec.Body.String(), "<dialog") {
		t.Errorf("le fragment htmx contient le layout")
	}
	if rec := e.c.do("GET", "/admin/calendar?date=oups", nil); rec.Code != 400 {
		t.Errorf("date invalide: %d", rec.Code)
	}
}

func TestCalendarCreateFlow(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)

	// Formulaire prérempli depuis le créneau cliqué.
	mustContain(t, e.c.do("GET", "/admin/appointments/new?start=2026-10-20T10:30", nil), 200,
		`value="2026-10-20"`, `value="10:30"`, `name="type_id"`, `for="appt-type"`, `name="force"`, `hx-trigger="input changed delay:300ms, search"`)

	// Sans JS : POST puis redirection vers la semaine concernée.
	rec := e.c.do("POST", "/admin/appointments", e.form("2026-10-20", "10:00"))
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/calendar?date=2026-10-20&view=week" {
		t.Fatalf("création: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	mustContain(t, e.c.do("GET", "/admin/calendar?view=week&date=2026-10-20", nil), 200, `Rendez-vous créé.`, `Durand Léa`)

	// Recherche du patient (fragment htmx), puis 2e RDV avec le patient existant.
	rec = e.c.doHXGet("/admin/patients/search?q=dur")
	mustContain(t, rec, 200, `id="patient-results"`, `Durand Léa`, `&#43;33612345678`, `name="patient_id"`)
	m := regexp.MustCompile(`name="patient_id" value="([0-9a-f-]{36})"`).FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("patient non trouvé")
	}
	if rec := e.c.doHXGet("/admin/patients/search?q=zzz"); !strings.Contains(rec.Body.String(), "Aucun patient trouvé") {
		t.Errorf("recherche vide: %s", rec.Body.String())
	}
	second := url.Values{"patient_id": {m[1]}, "type_id": {e.typeID.String()}, "date": {"2026-10-20"}, "time": {"11:00"}}
	if rec := e.c.do("POST", "/admin/appointments", second); rec.Code != 303 {
		t.Fatalf("2e création: %d %s", rec.Code, rec.Body.String())
	}

	// Conflit (buffer compris) : sans JS 422 avec la saisie conservée ; même forcé.
	conflict := e.form("2026-10-20", "10:35")
	conflict.Set("force", "1")
	mustContain(t, e.c.do("POST", "/admin/appointments", conflict), 422, `chevauche un autre rendez-vous`, `value="10:35"`, `role="alert"`)
	// htmx : 200 et fragment du formulaire seul.
	rec = e.c.doHX("POST", "/admin/appointments", conflict)
	mustContain(t, rec, 200, `id="appt-form"`, `chevauche un autre rendez-vous`)
	if strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("fragment avec layout")
	}
	// Hors horaires : refusé, accepté avec la case cochée.
	late := e.form("2026-10-20", "15:00")
	mustContain(t, e.c.do("POST", "/admin/appointments", late), 422, `hors des horaires`)
	late.Set("force", "1")
	if rec := e.c.do("POST", "/admin/appointments", late); rec.Code != 303 {
		t.Errorf("hors horaires forcé: %d", rec.Code)
	}
	// Succès htmx : redirection côté client.
	ok := e.form("2026-10-22", "09:00")
	if rec := e.c.doHX("POST", "/admin/appointments", ok); rec.Code != 204 || rec.Header().Get("HX-Redirect") != "/admin/calendar?date=2026-10-22&view=week" {
		t.Errorf("création htmx: %d %q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
	// Champs manquants.
	mustContain(t, e.c.do("POST", "/admin/appointments", url.Values{"date": {"2026-10-22"}, "time": {"09:00"}}), 422, `Choisissez un patient`, `Choisissez un motif`)
}

func TestCalendarMoveCancelStatusFlow(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	for _, c := range [][2]string{{"2026-10-20", "09:00"}, {"2026-10-20", "10:00"}} {
		if rec := e.c.do("POST", "/admin/appointments", e.form(c[0], c[1])); rec.Code != 303 {
			t.Fatalf("création %v: %d %s", c, rec.Code, rec.Body.String())
		}
	}
	id := e.firstAppointmentID(t, "2026-10-20")
	base := "/admin/appointments/" + id

	// Détail : page complète et fragment htmx.
	mustContain(t, e.c.do("GET", base, nil), 200, `Léa Durand`, `Séance 30`, `Saisi par le cabinet`, `Historique`, `Rendez-vous créé pour le`, `action="`+base+`/move"`, `action="`+base+`/cancel"`, `Marquer honoré`)
	frag := e.c.doHXGet(base)
	mustContain(t, frag, 200, `id="appt-detail"`)
	if strings.Contains(frag.Body.String(), "<html") {
		t.Errorf("fragment avec layout")
	}

	// Déplacement en conflit (sur l'autre RDV de 10:00, buffer compris) → erreur, rien ne change.
	mustContain(t, e.c.do("POST", base+"/move", url.Values{"date": {"2026-10-20"}, "time": {"09:45"}}), 422, `chevauche un autre rendez-vous`)
	mustContain(t, e.c.doHX("POST", base+"/move", url.Values{"date": {"2026-10-20"}, "time": {"09:45"}}), 200, `id="appt-detail"`, `chevauche un autre rendez-vous`)
	// Déplacement valide.
	rec := e.c.do("POST", base+"/move", url.Values{"date": {"2026-10-21"}, "time": {"08:30"}, "view": {"week"}})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/calendar?date=2026-10-21&view=week" {
		t.Fatalf("déplacement: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	mustContain(t, e.c.do("GET", base, nil), 200, `mercredi 21 octobre 2026`, `de 08h30 à 09h00`, `Déplacé du`)
	// Hors horaires non forcé.
	mustContain(t, e.c.do("POST", base+"/move", url.Values{"date": {"2026-10-21"}, "time": {"16:00"}}), 422, `hors des horaires`)

	// Statut.
	if rec := e.c.do("POST", base+"/status", url.Values{"status": {"no_show"}, "date": {"2026-10-21"}}); rec.Code != 303 {
		t.Fatalf("statut: %d", rec.Code)
	}
	mustContain(t, e.c.do("GET", base, nil), 200, `Absent`, `Statut : Absent`, `Remettre à venir`)
	mustContain(t, e.c.do("POST", base+"/status", url.Values{"status": {"nimporte"}}), 422, `Statut inconnu`)

	if rec := e.c.do("POST", base+"/status", url.Values{"status": {"booked"}, "date": {"2026-10-21"}}); rec.Code != 303 {
		t.Fatalf("statut booked: %d", rec.Code)
	}
	// Annulation avec motif, puis l'agenda ne l'affiche plus ; plus d'actions.
	rec = e.c.do("POST", base+"/cancel", url.Values{"reason": {"Patient souffrant"}, "date": {"2026-10-21"}})
	if rec.Code != 303 {
		t.Fatalf("annulation: %d %s", rec.Code, rec.Body.String())
	}
	body := e.c.do("GET", base, nil).Body.String()
	for _, want := range []string{"Annulé", "Patient souffrant"} {
		if !strings.Contains(body, want) {
			t.Errorf("détail annulé sans %q", want)
		}
	}
	if strings.Contains(body, "/move") || strings.Contains(body, "/cancel") {
		t.Errorf("actions encore proposées pour un RDV annulé")
	}
	if strings.Contains(e.c.do("GET", "/admin/calendar?view=week&date=2026-10-21", nil).Body.String(), base) {
		t.Errorf("RDV annulé encore dans l'agenda")
	}
	mustContain(t, e.c.do("POST", base+"/cancel", url.Values{}), 422, `Seul un rendez-vous à venir peut être annulé`)
}

func TestCalendarOtherPracticeIsNotFound(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, e.db, auth.CreateUserParams{Email: "autre@example.test", Role: domain.RoleAdmin, PracticeName: "Autre", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	other := service.Actor{UserID: u.ID, PracticeID: u.PracticeID}
	ts, _ := e.cal.ActiveTypes(ctx, other)
	ap, err := e.cal.Create(ctx, other, service.NewAppointmentInput{
		NewFirstName: "Zoé", NewLastName: "Autre", NewEmail: "zoe@example.test",
		TypeID: ts[0].ID.String(), Date: "2026-10-20", Time: "09:00", Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := "/admin/appointments/" + ap.ID.String()
	if rec := e.c.do("GET", base, nil); rec.Code != 404 {
		t.Errorf("détail: %d", rec.Code)
	}
	for _, p := range []string{"/move", "/cancel", "/status"} {
		form := url.Values{"date": {"2026-10-21"}, "time": {"09:00"}, "force": {"1"}, "status": {"done"}}
		if rec := e.c.do("POST", base+p, form); rec.Code != 404 {
			t.Errorf("POST %s : %d", p, rec.Code)
		}
	}
	if strings.Contains(e.c.do("GET", "/admin/calendar?view=week&date=2026-10-20", nil).Body.String(), "Autre Zoé") {
		t.Errorf("RDV d'un autre cabinet dans l'agenda")
	}
}

// 150 RDV sur une semaine : page complète (requête + calcul + rendu) en moins de 100 ms.
func TestCalendarWeekWith150AppointmentsIsFast(t *testing.T) {
	e := newCalendarEnv(t, domain.RoleAdmin)
	ctx := context.Background()
	first, err := e.cal.Create(ctx, e.actor, e.form2("2026-10-19", "07:00"))
	if err != nil {
		t.Fatal(err)
	}
	n := 1
	for day := 0; day < 7 && n < 150; day++ {
		d := time.Date(2026, 10, 19+day, 0, 0, 0, 0, time.UTC)
		for slot := 0; slot < 22 && n < 150; slot++ {
			if day == 0 && slot == 0 {
				continue
			}
			clock := time.Date(2000, 1, 1, 7, 0, 0, 0, time.UTC).Add(time.Duration(slot) * 45 * time.Minute)
			_, err := e.cal.Create(ctx, e.actor, service.NewAppointmentInput{
				PatientID: first.PatientID.String(), TypeID: e.typeID.String(),
				Date: d.Format("2006-01-02"), Time: clock.Format("15:04"), Force: true,
			})
			if err != nil {
				t.Fatalf("RDV %d: %v", n, err)
			}
			n++
		}
	}
	if n != 150 {
		t.Fatalf("%d RDV créés", n)
	}
	best := time.Hour
	for i := 0; i < 3; i++ {
		start := time.Now()
		rec := e.c.do("GET", "/admin/calendar?view=week&date=2026-10-21", nil)
		d := time.Since(start)
		if rec.Code != 200 || strings.Count(rec.Body.String(), `class="cal-appt `) != 150 {
			t.Fatalf("page: %d, %d RDV", rec.Code, strings.Count(rec.Body.String(), `class="cal-appt `))
		}
		best = min(best, d)
	}
	t.Logf("semaine de 150 RDV : %s", best)
	if best > 100*time.Millisecond {
		t.Errorf("agenda trop lent : %s (> 100 ms)", best)
	}
}

func (e calEnv) form2(date, clock string) service.NewAppointmentInput {
	return service.NewAppointmentInput{
		NewFirstName: "Léa", NewLastName: "Durand", NewPhone: "0612345678",
		TypeID: e.typeID.String(), Date: date, Time: clock, Force: true,
	}
}

// doNoCSRF envoie un POST authentifié sans jeton CSRF (ni champ ni en-tête).
func (c *client) doNoCSRF(method, target string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader("x=1"))
	req.RemoteAddr = c.addr + ":4321"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

// doHXGet envoie un GET htmx.
func (c *client) doHXGet(target string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	req.RemoteAddr = c.addr + ":4321"
	req.Header.Set("HX-Request", "true")
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}
