package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

const groupPractice = "Cabinet de groupe"

// groupEnv est un cabinet de groupe : un administrateur, un secrétariat et
// quatre praticiens, chacun avec son client HTTP connecté.
type groupEnv struct {
	h        http.Handler
	db       *pgxpool.Pool
	practice sqlcgen.Practice
	cal      *service.Calendar
	svc      *service.Practice
	admin    *client
	sec      *client
	prac     [4]*client
	pracIDs  [4]uuid.UUID // praticiens (table practitioner)
	pracUser [4]auth.User
	appts    [4]uuid.UUID // un RDV à 9 h le 15/10/2026 pour chacun
	typeID   uuid.UUID
}

func (e groupEnv) adminActor() service.Actor {
	return service.Actor{UserID: uuid.New(), PracticeID: e.practice.ID, Role: domain.RoleAdmin}
}

// newGroupEnv monte le cabinet de groupe ; l'horloge est figée au jeudi 15/10/2026 11:00 (Paris).
func newGroupEnv(t *testing.T) groupEnv {
	t.Helper()
	db := testutil.DB(t)
	ctx := context.Background()
	mk := func(email, role string) sqlcgen.AppUser {
		u, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: email, Role: role, PracticeName: groupPractice, Password: pw})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	adminU := mk("admin@example.test", domain.RoleAdmin)
	secU := mk("secretaire@example.test", domain.RoleSecretary)
	var pracU [4]sqlcgen.AppUser
	for i := range pracU {
		pracU[i] = mk(fmt.Sprintf("p%d@example.test", i+1), domain.RolePractitioner)
	}

	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cal := &service.Calendar{Pool: db, Clock: clock, Events: service.NopSink{}, ManageKey: service.NewManageKey(testutil.ManageSecret)}
	svc := &service.Practice{Pool: db, Clock: clock}
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev, BaseURL: "https://rdv.example.test"}, Logger: logger, DB: db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Practice: svc, Calendar: cal, Feed: &service.CalendarFeed{Pool: db, Clock: clock},
		Booking: &service.Booking{Pool: db, Clock: clock, ManageKey: service.NewManageKey(testutil.ManageSecret)},
	})
	e := groupEnv{h: h, db: db, cal: cal, svc: svc}
	e.practice = mustPractice(t, db, adminU.PracticeID)

	login := func(addr, email string) *client {
		c := newClient(t, h, addr)
		if rec := c.login(email, pw, "/admin"); rec.Code != 303 {
			t.Fatalf("login %s: %d", email, rec.Code)
		}
		return c
	}
	e.admin = login("192.0.2.31", adminU.Email)
	e.sec = login("192.0.2.32", secU.Email)

	ts, err := cal.ActiveTypes(ctx, e.adminActor())
	if err != nil || len(ts) == 0 {
		t.Fatalf("motifs: %v", err)
	}
	e.typeID = ts[0].ID
	var typeIDs []string
	for _, ty := range ts {
		typeIDs = append(typeIDs, ty.ID.String())
	}
	for i, u := range pracU {
		e.prac[i] = login(fmt.Sprintf("192.0.2.%d", 40+i), u.Email)
		actor := service.Actor{UserID: u.ID, PracticeID: u.PracticeID, Role: domain.RolePractitioner}
		pr, err := svc.Practitioner(ctx, actor) // crée le profil au premier accès
		if err != nil {
			t.Fatal(err)
		}
		e.pracIDs[i] = pr.ID
		e.pracUser[i] = auth.User{ID: u.ID, PracticeID: u.PracticeID, Role: u.Role, PractitionerID: uuid.NullUUID{UUID: pr.ID, Valid: true}}
		actor.PractitionerID = pr.ID
		if err := svc.AddRules(ctx, actor, service.RuleInput{Weekdays: []string{"0", "1", "2", "3", "4"}, Start: "08:00", End: "12:00", TypeIDs: typeIDs}); err != nil {
			t.Fatal(err)
		}
		ap, err := cal.Create(ctx, service.Actor{UserID: adminU.ID, PracticeID: u.PracticeID, Role: domain.RoleAdmin, Target: pr.ID}, service.NewAppointmentInput{
			NewFirstName: "Léa", NewLastName: fmt.Sprintf("Patient%d", i+1), NewPhone: "0612345678", TypeID: e.typeID.String(), Date: "2026-10-15", Time: "09:00",
		})
		if err != nil {
			t.Fatal(err)
		}
		e.appts[i] = ap.ID
	}
	return e
}

func mustPractice(t *testing.T, db *pgxpool.Pool, id uuid.UUID) sqlcgen.Practice {
	t.Helper()
	pc, err := sqlcgen.New(db).GetPractice(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return pc
}

func (e groupEnv) rulesOf(t *testing.T, practitioner uuid.UUID) int {
	t.Helper()
	rules, err := sqlcgen.New(e.db).ListAvailabilityRules(context.Background(), practitioner)
	if err != nil {
		t.Fatal(err)
	}
	return len(rules)
}

func (e groupEnv) ruleForm() url.Values {
	return url.Values{"weekday": {"5"}, "start": {"14:00"}, "end": {"16:00"}, "type": {e.typeID.String()}}
}

// Un praticien ne modifie pas les horaires d'un autre (403) ; l'administrateur
// le peut ; le secrétariat ne règle aucun horaire.
func TestPractitionerCannotEditAnotherAvailability(t *testing.T) {
	t.Parallel()
	e := newGroupEnv(t)
	other := e.pracIDs[1]
	before := e.rulesOf(t, other)

	p1 := e.prac[0]
	target := "?practitioner=" + other.String()
	if rec := p1.do("POST", "/admin/availability"+target, e.ruleForm()); rec.Code != 403 {
		t.Errorf("ajout chez un autre: %d", rec.Code)
	}
	if rec := p1.do("GET", "/admin/availability"+target, nil); rec.Code != 403 {
		t.Errorf("lecture chez un autre: %d", rec.Code)
	}
	if rec := p1.do("POST", "/admin/absences"+target, url.Values{"start_date": {"2026-11-02"}}); rec.Code != 403 {
		t.Errorf("absence chez un autre: %d", rec.Code)
	}
	// Même cible passée dans le formulaire.
	f := e.ruleForm()
	f.Set("practitioner", other.String())
	if rec := p1.do("POST", "/admin/availability", f); rec.Code != 403 {
		t.Errorf("ajout chez un autre (formulaire): %d", rec.Code)
	}
	// Suppression d'une plage d'un autre via son propre contexte : rien ne change.
	rules, _ := sqlcgen.New(e.db).ListAvailabilityRules(context.Background(), other)
	if rec := p1.do("POST", "/admin/availability/"+rules[0].ID.String()+"/delete", url.Values{}); rec.Code != 303 && rec.Code != 200 {
		t.Errorf("suppression (contexte propre): %d", rec.Code)
	}
	if got := e.rulesOf(t, other); got != before {
		t.Errorf("plages de l'autre praticien modifiées: %d -> %d", before, got)
	}

	// Sur ses propres horaires, explicitement ou non : accepté.
	if rec := p1.do("POST", "/admin/availability?practitioner="+e.pracIDs[0].String(), e.ruleForm()); rec.Code != 303 {
		t.Errorf("ajout chez soi (cible explicite): %d", rec.Code)
	}
	if rec := p1.do("POST", "/admin/availability", e.ruleForm()); rec.Code != 303 && rec.Code != 422 {
		t.Errorf("ajout chez soi: %d", rec.Code)
	}

	// L'administrateur règle les horaires de n'importe quel praticien.
	if rec := e.admin.do("POST", "/admin/availability"+target, e.ruleForm()); rec.Code != 303 {
		t.Fatalf("admin: %d", rec.Code)
	}
	if got := e.rulesOf(t, other); got != before+1 {
		t.Errorf("plages après ajout par l'admin: %d, attendu %d", got, before+1)
	}
	if rec := e.admin.do("GET", "/admin/availability"+target, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="practitioner-select"`) {
		t.Errorf("page horaires de l'admin: %d", rec.Code)
	}
	// Le secrétariat ne règle pas les horaires.
	if rec := e.sec.do("GET", "/admin/availability"+target, nil); rec.Code != 403 {
		t.Errorf("secrétariat, horaires: %d", rec.Code)
	}
	// Praticien inconnu ou d'un autre cabinet : 404 pour l'admin.
	if rec := e.admin.do("GET", "/admin/availability?practitioner="+uuid.NewString(), nil); rec.Code != 404 {
		t.Errorf("praticien inconnu: %d", rec.Code)
	}
	if rec := e.admin.do("GET", "/admin/availability?practitioner=pas-un-uuid", nil); rec.Code != 404 {
		t.Errorf("praticien invalide: %d", rec.Code)
	}
}

// Vue jour à 4 praticiens : une ressource par praticien pour le calendrier, et le repli (liste du jour) lisible côté HTML.
func TestDayViewWithFourPractitioners(t *testing.T) {
	t.Parallel()
	e := newGroupEnv(t)
	for _, tc := range []struct {
		name string
		c    *client
		path string
	}{
		{"admin, tous", e.admin, "/admin/calendar?view=day&date=2026-10-15&practitioner=all"},
		{"secrétariat, défaut", e.sec, "/admin/calendar?view=day&date=2026-10-15"},
		{"admin, sélection", e.admin, "/admin/calendar?view=day&date=2026-10-15" + fourIDs(e)},
	} {
		rec := tc.c.do("GET", tc.path, nil)
		body := rec.Body.String()
		if rec.Code != 200 {
			t.Fatalf("%s: %d", tc.name, rec.Code)
		}
		// Une ressource par praticien (les clics sur un créneau visent la colonne) ; la liste du jour a la colonne Praticien.
		if n := strings.Count(body, "&#34;title&#34;"); n != 4 || !strings.Contains(body, `scope="col" class="hidden sm:table-cell">Praticien<`) {
			t.Errorf("%s: %d ressources, colonne praticien absente", tc.name, n)
		}
		for i := 1; i <= 4; i++ {
			if !strings.Contains(body, e.pracIDs[i-1].String()) {
				t.Errorf("%s: ressource du praticien %d absente", tc.name, i)
			}
			if !strings.Contains(body, fmt.Sprintf("Patient%d", i)) {
				t.Errorf("%s: RDV du praticien %d absent", tc.name, i)
			}
		}
	}
	// Un praticien ne voit que son agenda, même en demandant « tous ».
	rec := e.prac[0].do("GET", "/admin/calendar?view=day&date=2026-10-15&practitioner=all", nil)
	if b := rec.Body.String(); rec.Code != 200 || strings.Count(b, "&#34;title&#34;") != 1 || strings.Contains(b, e.pracIDs[1].String()) || strings.Contains(b, "Patient2") {
		t.Errorf("praticien, tous: %d", rec.Code)
	}
	// Idem pour l'endpoint JSON : « all » se limite à son agenda.
	if rec := e.prac[0].do("GET", "/admin/agenda/events?start=2026-10-15&end=2026-10-16&practitioner=all", nil); rec.Code != 200 || strings.Contains(rec.Body.String(), "Patient2") {
		t.Errorf("praticien, tous (JSON): %d", rec.Code)
	}
	// Et se voit refuser l'agenda d'un autre.
	if rec := e.prac[0].do("GET", "/admin/calendar?view=day&practitioner="+e.pracIDs[1].String(), nil); rec.Code != 403 {
		t.Errorf("praticien, agenda d'un autre: %d", rec.Code)
	}
	// En vue semaine, une seule colonne praticien (7 jours).
	rec = e.admin.do("GET", "/admin/calendar?view=week&date=2026-10-15&practitioner=all", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `data-view="week"`) {
		t.Errorf("vue semaine multi: %d", rec.Code)
	}
}

func fourIDs(e groupEnv) string {
	var s string
	for _, id := range e.pracIDs {
		s += "&practitioner=" + id.String()
	}
	return s
}

// Le secrétariat gère les RDV de tous ; un praticien ne touche pas à ceux des autres.
func TestAppointmentAccessByRole(t *testing.T) {
	t.Parallel()
	e := newGroupEnv(t)
	own, foreign := e.appts[0], e.appts[1]

	if rec := e.prac[0].do("GET", "/admin/appointments/"+own.String(), nil); rec.Code != 200 {
		t.Errorf("praticien, son RDV: %d", rec.Code)
	}
	if rec := e.prac[0].do("GET", "/admin/appointments/"+foreign.String(), nil); rec.Code != 403 {
		t.Errorf("praticien, RDV d'un autre: %d", rec.Code)
	}
	for _, action := range []struct {
		path string
		form url.Values
	}{
		{"cancel", url.Values{"reason": {"x"}}},
		{"status", url.Values{"status": {"done"}}},
		{"move", url.Values{"date": {"2026-10-16"}, "time": {"09:00"}}},
	} {
		if rec := e.prac[0].do("POST", "/admin/appointments/"+foreign.String()+"/"+action.path, action.form); rec.Code != 403 {
			t.Errorf("praticien, %s sur un RDV d'un autre: %d", action.path, rec.Code)
		}
	}
	var status string
	if err := e.db.QueryRow(context.Background(), `SELECT status FROM appointment WHERE id = $1`, foreign).Scan(&status); err != nil || status != "booked" {
		t.Errorf("RDV d'un autre modifié: %q, %v", status, err)
	}
	if rec := e.sec.do("GET", "/admin/appointments/"+foreign.String(), nil); rec.Code != 200 {
		t.Errorf("secrétariat, détail: %d", rec.Code)
	}
	if rec := e.sec.do("POST", "/admin/appointments/"+foreign.String()+"/status", url.Values{"status": {"done"}}); rec.Code != 303 {
		t.Errorf("secrétariat, statut: %d", rec.Code)
	}

	// Création : le secrétariat doit viser un praticien ; un praticien ne vise pas un autre.
	form := url.Values{
		"new_last_name": {"Durand"}, "new_first_name": {"Léa"}, "new_phone": {"0612345678"}, "type_id": {e.typeID.String()},
		"date": {"2026-10-16"}, "time": {"10:00"}, "view": {"day"}, "force": {"1"},
	}
	if rec := e.sec.do("POST", "/admin/appointments", cloneValues(form)); rec.Code != 403 {
		t.Errorf("secrétariat, sans praticien: %d", rec.Code)
	}
	f := cloneValues(form)
	f.Set("practitioner", e.pracIDs[2].String())
	if rec := e.sec.do("POST", "/admin/appointments", f); rec.Code != 303 {
		t.Fatalf("secrétariat, pour le praticien 3: %d", rec.Code)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM appointment WHERE practitioner_id = $1 AND source = 'staff'`, e.pracIDs[2]).Scan(&n); err != nil || n != 2 {
		t.Errorf("RDV du praticien 3: %d, %v", n, err)
	}
	f.Set("practitioner", e.pracIDs[1].String())
	if rec := e.prac[0].do("POST", "/admin/appointments", f); rec.Code != 403 {
		t.Errorf("praticien, création chez un autre: %d", rec.Code)
	}
	if rec := e.prac[0].do("GET", "/admin/appointments/new?practitioner="+e.pracIDs[1].String(), nil); rec.Code != 403 {
		t.Errorf("praticien, formulaire chez un autre: %d", rec.Code)
	}
	if rec := e.sec.do("GET", "/admin/appointments/new?practitioner="+e.pracIDs[1].String(), nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="appt-practitioner"`) {
		t.Errorf("secrétariat, formulaire: %d", rec.Code)
	}
	// Le secrétariat n'a pas accès aux réglages, à l'équipe ni aux séries.
	for _, p := range []string{"/admin/settings", "/admin/appointment-types", "/admin/absences", "/admin/team"} {
		if rec := e.sec.do("GET", p, nil); rec.Code != 403 {
			t.Errorf("secrétariat, %s: %d", p, rec.Code)
		}
	}
	if rec := e.sec.do("GET", "/admin", nil); rec.Code != 200 || strings.Contains(rec.Body.String(), "/admin/settings") {
		t.Errorf("menu du secrétariat: %d", rec.Code)
	}
	if rec := e.admin.do("GET", "/admin", nil); !strings.Contains(rec.Body.String(), `href="/admin/team"`) {
		t.Errorf("menu de l'admin sans Équipe")
	}
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// Aucun accès croisé entre cabinets, même avec des identifiants valides.
func TestCrossPracticeAccessIsDenied(t *testing.T) {
	t.Parallel()
	e := newGroupEnv(t)
	ctx := context.Background()
	foreign := testutil.NewPractice(t, e.db)
	fpr := testutil.NewPractitioner(t, e.db, foreign.ID)
	fpat := testutil.NewPatient(t, e.db, foreign.ID)
	fty := testutil.NewAppointmentType(t, e.db, foreign.ID)
	start := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	fap, err := insertForeignAppointment(ctx, e, foreign.ID, fpr.ID, fpat.ID, fty, start)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name string
		cl   *client
	}{{"admin", e.admin}, {"secrétariat", e.sec}, {"praticien", e.prac[0]}} {
		for _, p := range []string{
			"/admin/calendar?view=day&practitioner=" + fpr.ID.String(),
			"/admin/availability?practitioner=" + fpr.ID.String(),
			"/admin/appointments/new?practitioner=" + fpr.ID.String(),
			"/admin/appointments/" + fap.String(),
		} {
			rec := c.cl.do("GET", p, nil)
			if rec.Code != 403 && rec.Code != 404 {
				t.Errorf("%s GET %s: %d", c.name, p, rec.Code)
			}
		}
		for _, a := range []string{"cancel", "status", "move"} {
			rec := c.cl.do("POST", "/admin/appointments/"+fap.String()+"/"+a, url.Values{"status": {"done"}, "date": {"2026-10-17"}, "time": {"09:00"}})
			if rec.Code != 403 && rec.Code != 404 {
				t.Errorf("%s POST %s: %d", c.name, a, rec.Code)
			}
		}
	}
	var status string
	if err := e.db.QueryRow(ctx, `SELECT status FROM appointment WHERE id = $1`, fap).Scan(&status); err != nil || status != "booked" {
		t.Errorf("RDV étranger modifié: %q, %v", status, err)
	}
	// La création visant un praticien d'un autre cabinet échoue aussi.
	f := url.Values{"new_last_name": {"X"}, "new_first_name": {"Y"}, "new_phone": {"0612345678"}, "type_id": {e.typeID.String()}, "date": {"2026-10-16"}, "time": {"10:00"}, "force": {"1"}, "practitioner": {fpr.ID.String()}}
	if rec := e.admin.do("POST", "/admin/appointments", f); rec.Code != 404 {
		t.Errorf("admin, création chez un praticien étranger: %d", rec.Code)
	}
}

func insertForeignAppointment(ctx context.Context, e groupEnv, practiceID, practitionerID, patientID uuid.UUID, ty sqlcgen.AppointmentType, start time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := e.db.QueryRow(ctx, `
		INSERT INTO appointment (practice_id, practitioner_id, patient_id, appointment_type_id, during, during_blocked, status, source)
		VALUES ($1, $2, $3, $4, tstzrange($5::timestamptz, $5::timestamptz + interval '30 minutes', '[)'), tstzrange($5::timestamptz, $5::timestamptz + interval '30 minutes', '[)'), 'booked', 'staff')
		RETURNING id`, practiceID, practitionerID, patientID, ty.ID, start).Scan(&id)
	return id, err
}

func TestTeamPageAccess(t *testing.T) {
	t.Parallel()
	e := newGroupEnv(t)
	anon := newClient(t, e.h, "192.0.2.60")
	if rec := anon.do("GET", "/admin/team", nil); rec.Code != 303 {
		t.Errorf("anonyme: %d", rec.Code)
	}
	for name, c := range map[string]*client{"secrétariat": e.sec, "praticien": e.prac[0]} {
		if rec := c.do("GET", "/admin/team", nil); rec.Code != 403 {
			t.Errorf("%s GET: %d", name, rec.Code)
		}
		if rec := c.do("POST", "/admin/team/invite", url.Values{"email": {"x@example.test"}, "role": {"secretary"}}); rec.Code != 403 {
			t.Errorf("%s invite: %d", name, rec.Code)
		}
		if rec := c.do("POST", "/admin/team/"+e.pracUser[1].ID.String()+"/active", url.Values{"active": {"0"}}); rec.Code != 403 {
			t.Errorf("%s désactive: %d", name, rec.Code)
		}
	}
	rec := e.admin.do("GET", "/admin/team", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "p1@example.test") || !strings.Contains(rec.Body.String(), "secretaire@example.test") {
		t.Errorf("admin: %d", rec.Code)
	}
}

// Parcours complet : invitation par l'admin, email dans l'outbox, activation par
// le lien (usage unique), connexion ; puis désactivation et réactivation.
func TestInvitationFlowAndDeactivation(t *testing.T) {
	t.Parallel()
	e := newGroupEnv(t)
	ctx := context.Background()

	if rec := e.admin.do("POST", "/admin/team/invite", url.Values{"email": {"invitee"}, "role": {"secretary"}}); rec.Code != 422 {
		t.Errorf("email invalide: %d", rec.Code)
	}
	if rec := e.admin.do("POST", "/admin/team/invite", url.Values{"email": {"nouvelle@example.test"}, "role": {"secretary"}}); rec.Code != 303 {
		t.Fatalf("invitation: %d", rec.Code)
	}
	var payload []byte
	if err := e.db.QueryRow(ctx, `SELECT payload FROM outbox_message WHERE template = 'user_invitation' AND recipient = 'nouvelle@example.test'`).Scan(&payload); err != nil {
		t.Fatalf("email d'invitation: %v", err)
	}
	var p map[string]string
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(p["accept_url"])
	if err != nil || link.Host != "rdv.example.test" || link.Path != auth.AcceptPath {
		t.Fatalf("accept_url = %q", p["accept_url"])
	}
	token := link.Query().Get("token")
	if rec := e.admin.do("GET", "/admin/team", nil); !strings.Contains(rec.Body.String(), "Invitations en attente") {
		t.Errorf("invitation non listée")
	}

	invitee := newClient(t, e.h, "192.0.2.61")
	page := invitee.do("GET", auth.AcceptPath+"?token="+url.QueryEscape(token), nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "nouvelle@example.test") || !strings.Contains(page.Body.String(), `name="password"`) {
		t.Fatalf("page d'activation: %d", page.Code)
	}
	if rec := invitee.do("POST", auth.AcceptPath, url.Values{"token": {token}, "password": {pw}, "password2": {"autre-chose-12345"}}); rec.Code != 422 {
		t.Errorf("mots de passe différents: %d", rec.Code)
	}
	if rec := invitee.do("POST", auth.AcceptPath, url.Values{"token": {token}, "password": {"court"}, "password2": {"court"}}); rec.Code != 422 {
		t.Errorf("mot de passe faible: %d", rec.Code)
	}
	if rec := invitee.do("POST", auth.AcceptPath, url.Values{"token": {token}, "password": {pw}, "password2": {pw}}); rec.Code != 303 || rec.Header().Get("Location") != auth.LoginPath {
		t.Fatalf("activation: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// Usage unique.
	if rec := invitee.do("GET", auth.AcceptPath+"?token="+url.QueryEscape(token), nil); rec.Code != 400 {
		t.Errorf("lien réutilisé: %d", rec.Code)
	}
	if rec := newClient(t, e.h, "192.0.2.64").do("GET", auth.AcceptPath+"?token=inconnu", nil); rec.Code != 400 {
		t.Errorf("lien inconnu: %d", rec.Code)
	}
	if rec := invitee.login("nouvelle@example.test", pw, "/admin/calendar"); rec.Code != 303 {
		t.Fatalf("connexion du nouveau membre: %d", rec.Code)
	}
	if rec := invitee.do("GET", "/admin/calendar", nil); rec.Code != 200 {
		t.Errorf("agenda du nouveau secrétaire: %d", rec.Code)
	}

	// Désactivation d'un praticien : sa session tombe, sa connexion échoue.
	victim := e.pracUser[1]
	if rec := e.admin.do("POST", "/admin/team/"+victim.ID.String()+"/active", url.Values{"active": {"0"}}); rec.Code != 303 {
		t.Fatalf("désactivation: %d", rec.Code)
	}
	if rec := e.prac[1].do("GET", "/admin/calendar", nil); rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), auth.LoginPath) {
		t.Errorf("session d'un compte désactivé: %d", rec.Code)
	}
	if rec := newClient(t, e.h, "192.0.2.62").login("p2@example.test", pw, "/admin"); rec.Code != 401 {
		t.Errorf("connexion d'un compte désactivé: %d", rec.Code)
	}
	// L'administrateur ne peut pas se désactiver ni se rétrograder s'il est le seul.
	adminID := e.adminID(t)
	if rec := e.admin.do("POST", "/admin/team/"+adminID.String()+"/active", url.Values{"active": {"0"}}); rec.Code != 422 {
		t.Errorf("auto-désactivation: %d", rec.Code)
	}
	if rec := e.admin.do("POST", "/admin/team/"+adminID.String()+"/role", url.Values{"role": {"secretary"}}); rec.Code != 422 {
		t.Errorf("rétrogradation du dernier admin: %d", rec.Code)
	}
	// Changement de rôle d'un praticien en secrétariat : il perd les réglages, gagne tous les agendas.
	other := e.pracUser[2]
	if rec := e.admin.do("POST", "/admin/team/"+other.ID.String()+"/role", url.Values{"role": {"secretary"}}); rec.Code != 303 {
		t.Fatalf("changement de rôle: %d", rec.Code)
	}
	if rec := e.prac[2].do("GET", "/admin/settings", nil); rec.Code != 403 {
		t.Errorf("ancien praticien, réglages: %d", rec.Code)
	}
	if rec := e.prac[2].do("GET", "/admin/calendar?view=day&practitioner=all", nil); rec.Code != 200 {
		t.Errorf("ancien praticien, agendas: %d", rec.Code)
	}
	// Réactivation.
	if rec := e.admin.do("POST", "/admin/team/"+victim.ID.String()+"/active", url.Values{"active": {"1"}}); rec.Code != 303 {
		t.Fatalf("réactivation: %d", rec.Code)
	}
	if rec := newClient(t, e.h, "192.0.2.63").login("p2@example.test", pw, "/admin"); rec.Code != 303 {
		t.Errorf("connexion après réactivation: %d", rec.Code)
	}
	// Compte d'un autre cabinet : introuvable.
	foreign, err := auth.CreateUser(ctx, e.db, auth.CreateUserParams{Email: "etranger@example.test", Role: domain.RoleAdmin, PracticeName: "Ailleurs", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.admin.do("POST", "/admin/team/"+foreign.ID.String()+"/active", url.Values{"active": {"0"}}); rec.Code != 404 {
		t.Errorf("désactivation cross-cabinet: %d", rec.Code)
	}
}

func (e groupEnv) adminID(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := e.db.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = 'admin@example.test'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// « Premier disponible » : le créneau le plus proche, tous praticiens confondus.
func TestFirstAvailablePractitioner(t *testing.T) {
	t.Parallel()
	e := newGroupEnv(t)
	ctx := context.Background()
	anon := newClient(t, e.h, "192.0.2.70")
	base := "/p/" + e.practice.Slug

	page := anon.do("GET", base, nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Premier créneau disponible") || !strings.Contains(page.Body.String(), "/premier-disponible?type=") {
		t.Fatalf("page publique: %d", page.Code)
	}
	for i := 1; i <= 4; i++ {
		if !strings.Contains(page.Body.String(), fmt.Sprintf("/p/%s/p%d", e.practice.Slug, i)) {
			t.Errorf("praticien %d absent de la liste", i)
		}
	}

	// Vendredi 16/10 : tous fermés sauf le praticien 3 (absence pour les autres).
	for i, id := range e.pracIDs {
		if i == 2 {
			continue
		}
		a := service.Actor{UserID: uuid.New(), PracticeID: e.practice.ID, Role: domain.RoleAdmin, Target: id}
		if err := e.svc.AddClosure(ctx, a, service.ClosureInput{StartDate: "2026-10-16", EndDate: "2026-10-16"}); err != nil {
			t.Fatal(err)
		}
	}
	rec := anon.do("GET", base+"/premier-disponible?type="+e.typeID.String(), nil)
	loc := rec.Header().Get("Location")
	if rec.Code != 303 || !strings.HasPrefix(loc, base+"/p3/book/details?") {
		t.Fatalf("redirection = %d %q", rec.Code, loc)
	}
	u, _ := url.Parse(loc)
	if got := u.Query().Get("start"); got != "2026-10-16T06:00:00Z" || u.Query().Get("type") != e.typeID.String() {
		t.Errorf("créneau = %q type = %q", got, u.Query().Get("type"))
	}
	// Le formulaire de réservation accepte ce créneau.
	if rec := anon.do("GET", loc, nil); rec.Code != 200 {
		t.Errorf("formulaire de réservation: %d", rec.Code)
	}
	// Motif inconnu : 404 ; motif absent : retour à la page du cabinet.
	if rec := anon.do("GET", base+"/premier-disponible?type="+uuid.NewString(), nil); rec.Code != 404 {
		t.Errorf("motif inconnu: %d", rec.Code)
	}
	if rec := anon.do("GET", base+"/premier-disponible", nil); rec.Code != 303 || rec.Header().Get("Location") != base {
		t.Errorf("sans motif: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// Le segment est réservé : un praticien ne peut pas prendre ce slug.
	err := e.svc.UpdateSettings(ctx, service.Actor{UserID: e.pracUser[0].ID, PracticeID: e.practice.ID, Role: domain.RolePractitioner, PractitionerID: e.pracIDs[0]}, service.SettingsInput{
		Name: "Cabinet", BookingNoticeHours: "2", CancelNoticeHours: "24", HorizonDays: "60",
		FirstName: "A", LastName: "B", Slug: service.ReservedFirstSlug,
	})
	if len(domain.FieldErrors(err)) == 0 {
		t.Errorf("slug réservé accepté: %v", err)
	}
}
