package web_test

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
)

var todayCardID = regexp.MustCompile(`/admin/today/appointments/([0-9a-f-]{36})/status`)

func TestTodayPageListsDayAndPointsAppointments(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	ctx := context.Background()
	for _, hm := range []string{"10:00", "09:00"} {
		if rec := e.c.do("POST", "/admin/appointments", e.form("2026-10-20", hm)); rec.Code != 303 {
			t.Fatalf("création %s: %d", hm, rec.Code)
		}
	}
	// Une visite à domicile à 11:00, avec adresse.
	home, err := sqlcgen.New(e.db).CreateAppointmentType(ctx, sqlcgen.CreateAppointmentTypeParams{
		PracticeID: e.actor.PracticeID, Name: "Visite", AtHome: true,
		Duration:     pgtype.Interval{Microseconds: (45 * time.Minute).Microseconds(), Valid: true},
		BufferBefore: pgtype.Interval{Valid: true}, BufferAfter: pgtype.Interval{Valid: true}, Color: "#16a34a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.cal.Create(ctx, e.actor, service.NewAppointmentInput{
		NewFirstName: "Paul", NewLastName: "Martin", NewPhone: "0611223344", TypeID: home.ID.String(),
		Date: "2026-10-20", Time: "11:00", Address: "12 rue des Lilas, 75011 Paris", Force: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Page complète : ordre chronologique, téléphone cliquable, liens domicile, itinéraire, récapitulatif.
	rec := e.c.do("GET", "/admin/today?date=2026-10-20", nil)
	mustContain(t, rec, 200, `Ma journée`, `href="tel:&#43;33612345678"`, `href="geo:0,0?q=12&#43;rue&#43;des&#43;Lilas`,
		`openstreetmap.org/search?query=12&#43;rue&#43;des&#43;Lilas`, `openstreetmap.org/directions?route=12&#43;rue&#43;des&#43;Lilas`,
		`1 visite à domicile`, `Récapitulatif`, `Faites`, `Absents`, `À venir`, `href="/admin/today?date=2026-10-19`, `href="/admin/today?date=2026-10-21`,
		`name="csrf_token"`)
	body := rec.Body.String()
	if i, j := strings.Index(body, "Léa Durand"), strings.Index(body, "Paul Martin"); i < 0 || j < i {
		t.Errorf("ordre des cartes")
	}
	if strings.Index(body, "09h00") > strings.Index(body, "10h00") {
		t.Errorf("09h00 doit précéder 10h00")
	}
	ids := todayCardID.FindAllStringSubmatch(body, -1)
	if len(ids) == 0 {
		t.Fatal("aucune carte")
	}
	id := ids[0][1]
	statusURL := "/admin/today/appointments/" + id + "/status"

	// htmx : la carte seule + le récapitulatif en échange hors-bande.
	hx := e.c.doHX("POST", statusURL, url.Values{"status": {"done"}, "date": {"2026-10-20"}})
	mustContain(t, hx, 200, `id="day-card-`+id+`"`, `Annuler le pointage`, `hx-swap-oob="true"`, `Honoré`)
	if strings.Contains(hx.Body.String(), "<html") || strings.Count(hx.Body.String(), "<article") != 1 {
		t.Errorf("la réponse htmx doit contenir une seule carte: %s", hx.Body.String())
	}
	mustContain(t, e.c.do("GET", "/admin/today?date=2026-10-20", nil), 200, `Honoré`)

	// Sans JS : redirection vers la journée.
	rec = e.c.do("POST", statusURL, url.Values{"status": {"no_show"}, "date": {"2026-10-20"}})
	if rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/today?date=2026-10-20") {
		t.Errorf("redirection: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	mustContain(t, e.c.do("GET", "/admin/today?date=2026-10-20", nil), 200, `Absent`)

	// Le pointage est attribué à l'utilisateur connecté.
	apID := uuid.MustParse(id)
	ap, err := sqlcgen.New(e.db).GetAppointment(ctx, sqlcgen.GetAppointmentParams{ID: apID, PracticeID: e.actor.PracticeID})
	if err != nil {
		t.Fatal(err)
	}
	if !ap.StatusChangedBy.Valid || uuid.UUID(ap.StatusChangedBy.Bytes) != e.actor.UserID || ap.Status != "no_show" {
		t.Errorf("status_changed_by: %+v statut %s", ap.StatusChangedBy, ap.Status)
	}

	// Statut inconnu : refusé.
	if rec := e.c.do("POST", statusURL, url.Values{"status": {"weird"}}); rec.Code != 422 {
		t.Errorf("statut inconnu: %d", rec.Code)
	}
}

func TestTodayDefaultsToCurrentDayAndAccess(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	// Horloge figée au 15 octobre 2026.
	mustContain(t, e.c.do("GET", "/admin/today", nil), 200, `jeudi 15 octobre`, `Aucun rendez-vous ce jour`)
	if rec := e.c.do("GET", "/admin/today?date=nope", nil); rec.Code != 400 {
		t.Errorf("date invalide: %d", rec.Code)
	}
	if rec := e.c.do("GET", "/admin/today?practitioner=nope", nil); rec.Code != 404 {
		t.Errorf("praticien invalide: %d", rec.Code)
	}
	anon := newClient(t, e.h, "192.0.2.22")
	if rec := anon.do("GET", "/admin/today", nil); rec.Code != 303 {
		t.Errorf("sans session: %d", rec.Code)
	}
	if rec := e.c.doNoCSRF("POST", "/admin/today/appointments/"+uuid.NewString()+"/status"); rec.Code != 403 {
		t.Errorf("sans CSRF: %d", rec.Code)
	}
}

func TestTodayForbiddenForOtherPractitionerAndSecretaryPicker(t *testing.T) {
	t.Parallel()
	p := newCalendarEnv(t, domain.RolePractitioner)
	if rec := p.c.do("GET", "/admin/today?practitioner="+uuid.NewString(), nil); rec.Code != 403 {
		t.Errorf("praticien sur l'agenda d'un autre: %d", rec.Code)
	}
	mustContain(t, p.c.do("GET", "/admin/today", nil), 200, `Ma journée`)

	// Pointer le RDV d'un autre praticien : 403 aussi (via SetStatus).
	adm := newCalendarEnv(t, domain.RoleAdmin)
	if rec := adm.c.do("POST", "/admin/appointments", adm.form("2026-10-20", "09:00")); rec.Code != 303 {
		t.Fatalf("création: %d", rec.Code)
	}
	id := todayCardID.FindStringSubmatch(adm.c.do("GET", "/admin/today?date=2026-10-20", nil).Body.String())[1]
	if rec := p.c.do("POST", "/admin/today/appointments/"+id+"/status", url.Values{"status": {"done"}}); rec.Code != 404 && rec.Code != 403 {
		t.Errorf("RDV d'un autre cabinet: %d", rec.Code)
	}

	// Secrétariat : pas de praticien propre, mais la page s'affiche (sélecteur si plusieurs praticiens).
	sec := newCalendarEnv(t, domain.RoleSecretary)
	mustContain(t, sec.c.do("GET", "/admin/today", nil), 200, `Ma journée`)
}
