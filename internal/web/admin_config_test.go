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

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

// newConfigAdmin monte le handler complet avec le service de configuration et
// retourne un client connecté.
func newConfigAdmin(t *testing.T, role string) (http.Handler, *client) {
	t.Helper()
	db := testutil.DB(t)
	if _, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{
		Email: userEmail, Role: role, PracticeName: "Cabinet test", Password: pw,
	}); err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := web.Routes(web.Deps{
		Config:   config.Config{Env: config.EnvDev},
		Logger:   logger,
		DB:       db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Practice: &service.Practice{Pool: db, Clock: clock},
	})
	c := newClient(t, h, "192.0.2.10")
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	return h, c
}

// doHX envoie une requête htmx (HX-Request) avec le CSRF du client.
func (c *client) doHX(method, target string, form url.Values) *httptest.ResponseRecorder {
	c.t.Helper()
	form.Set("csrf_token", c.cookies["csrf_token"].Value)
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.RemoteAddr = c.addr + ":4321"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func mustContain(t *testing.T, rec *httptest.ResponseRecorder, status int, fragments ...string) {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != status {
		t.Fatalf("statut %d, attendu %d\n%s", rec.Code, status, body)
	}
	for _, f := range fragments {
		if !strings.Contains(body, f) {
			t.Errorf("fragment manquant %q", f)
		}
	}
}

func TestConfigPagesRequireLogin(t *testing.T) {
	t.Parallel()
	h, _ := newConfigAdmin(t, domain.RoleAdmin)
	anon := newClient(t, h, "192.0.2.11")
	for _, p := range []string{"/admin/settings", "/admin/appointment-types", "/admin/availability", "/admin/absences"} {
		if rec := anon.do("GET", p, nil); rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/login") {
			t.Errorf("GET %s sans session: %d %q", p, rec.Code, rec.Header().Get("Location"))
		}
		if rec := anon.do("POST", p, url.Values{}); rec.Code != 303 {
			t.Errorf("POST %s sans session: %d", p, rec.Code)
		}
	}
}

func TestConfigPostRequiresCSRF(t *testing.T) {
	t.Parallel()
	h, c := newConfigAdmin(t, domain.RoleAdmin)
	for _, p := range []string{"/admin/settings", "/admin/appointment-types", "/admin/availability", "/admin/absences"} {
		req := httptest.NewRequest("POST", p, strings.NewReader("name=x"))
		req.RemoteAddr = c.addr + ":4321"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, ck := range c.cookies {
			req.AddCookie(ck)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Errorf("POST %s sans jeton CSRF: %d", p, rec.Code)
		}
	}
}

func TestConfigSecretaryForbidden(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleSecretary)
	for _, p := range []string{"/admin/settings", "/admin/appointment-types", "/admin/availability", "/admin/absences"} {
		if rec := c.do("GET", p, nil); rec.Code != 403 {
			t.Errorf("GET %s secrétaire: %d", p, rec.Code)
		}
	}
}

func TestSettingsPage(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)

	mustContain(t, c.do("GET", "/admin/settings", nil), 200, `value="Cabinet test"`, `name="booking_notice_hours" type="number" inputmode="numeric" min="0" max="720" value="2"`, `name="slug"`, `for="first_name"`)

	// Invalide : statut 422, erreurs affichées, saisie conservée.
	rec := c.do("POST", "/admin/settings", url.Values{
		"name": {""}, "booking_notice_hours": {"abc"}, "cancel_notice_hours": {"24"}, "horizon_days": {"60"},
		"first_name": {"Camille"}, "last_name": {"Martin"}, "slug": {"camille"}, "phone": {"0102030405"},
	})
	mustContain(t, rec, 422, "Le nom du cabinet est requis.", "délai minimal de réservation", `value="Camille"`, `role="alert"`)

	// Valide : redirection 303 puis valeurs relues.
	rec = c.do("POST", "/admin/settings", url.Values{
		"name": {"Cabinet Étoile"}, "address": {"1 rue de la Paix"}, "phone": {"01 02 03 04 05"},
		"booking_notice_hours": {"6"}, "cancel_notice_hours": {"48"}, "horizon_days": {"45"},
		"first_name": {"Camille"}, "last_name": {"Martin"}, "title": {"Kiné"}, "bio": {"Bio"}, "slug": {"camille-martin"},
	})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/settings" {
		t.Fatalf("POST valide: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	mustContain(t, c.do("GET", "/admin/settings", nil), 200, "Paramètres enregistrés.", `value="Cabinet Étoile"`, `value="45"`, `value="camille-martin"`)
}

func TestAppointmentTypesPage(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)

	rec := c.do("GET", "/admin/appointment-types", nil)
	mustContain(t, rec, 200, "Bilan initial", "Séance à domicile", "45 min", `name="duration"`, `for="type-name"`)

	// Invalide : durée nulle.
	rec = c.do("POST", "/admin/appointment-types", url.Values{"name": {"Drainage"}, "duration": {"0"}})
	mustContain(t, rec, 422, "La durée doit être", `value="Drainage"`)

	// Valide sans JS : 303 + flash.
	rec = c.do("POST", "/admin/appointment-types", url.Values{"name": {"Drainage"}, "duration": {"50"}, "online_bookable": {"1"}, "color": {"#112233"}})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/appointment-types" {
		t.Fatalf("création: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = c.do("GET", "/admin/appointment-types", nil)
	mustContain(t, rec, 200, "Motif créé.", "Drainage", "50 min")

	// Édition : formulaire prérempli puis mise à jour.
	id := regexp.MustCompile(`/admin/appointment-types/([0-9a-f-]{36})"`).FindStringSubmatch(rec.Body.String())
	if id == nil {
		t.Fatal("lien de modification introuvable")
	}
	mustContain(t, c.do("GET", "/admin/appointment-types/"+id[1], nil), 200, "Modifier le motif", `action="/admin/appointment-types/`+id[1]+`"`)
	rec = c.do("POST", "/admin/appointment-types/"+id[1], url.Values{"name": {"Bilan"}, "duration": {"40"}})
	if rec.Code != 303 {
		t.Fatalf("mise à jour: %d", rec.Code)
	}
	if rec := c.do("GET", "/admin/appointment-types/not-a-uuid", nil); rec.Code != 404 {
		t.Errorf("id invalide: %d", rec.Code)
	}

	// Désactivation et déplacement.
	if rec := c.do("POST", "/admin/appointment-types/"+id[1]+"/active", url.Values{"active": {"0"}}); rec.Code != 303 {
		t.Fatalf("désactivation: %d", rec.Code)
	}
	mustContain(t, c.do("GET", "/admin/appointment-types", nil), 200, "(désactivé)", "Motif désactivé.")
	if rec := c.do("POST", "/admin/appointment-types/"+id[1]+"/move", url.Values{"dir": {"up"}}); rec.Code != 303 {
		t.Fatalf("déplacement: %d", rec.Code)
	}

	// htmx : panneau seul (pas de layout), erreur en 200 pour que htmx l'échange.
	rec = c.doHX("POST", "/admin/appointment-types", url.Values{"name": {""}, "duration": {"30"}})
	mustContain(t, rec, 200, `id="types-panel"`, "Le nom du motif est requis.")
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("réponse htmx avec layout complet")
	}
	rec = c.doHX("POST", "/admin/appointment-types", url.Values{"name": {"Autre"}, "duration": {"20"}})
	mustContain(t, rec, 200, `id="types-panel"`, "Motif créé.", "Autre")
}

func TestAvailabilityPage(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)

	mustContain(t, c.do("GET", "/admin/availability", nil), 200, "Lundi", "Dimanche", "Fermé", `name="weekday"`, `name="type"`)

	form := func(days []string, start, end string) url.Values {
		v := url.Values{"start": {start}, "end": {end}, "weekday": days}
		rec := c.do("GET", "/admin/availability", nil)
		for _, m := range regexp.MustCompile(`name="type" value="([0-9a-f-]{36})"`).FindAllStringSubmatch(rec.Body.String(), -1) {
			v.Add("type", m[1])
		}
		return v
	}

	rec := c.do("POST", "/admin/availability", form([]string{"0", "1", "2"}, "08:00", "12:00"))
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/availability" {
		t.Fatalf("ajout: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = c.do("POST", "/admin/availability", form([]string{"0", "1", "2"}, "14:00", "19:00"))
	if rec.Code != 303 {
		t.Fatalf("ajout après-midi: %d", rec.Code)
	}
	page := c.do("GET", "/admin/availability", nil)
	mustContain(t, page, 200, "08:00 – 12:00", "14:00 – 19:00", "Plages ajoutées.")

	// Chevauchement : erreur lisible, saisie conservée.
	rec = c.do("POST", "/admin/availability", form([]string{"0"}, "11:00", "15:00"))
	mustContain(t, rec, 422, "Lundi : la plage 11:00–15:00 chevauche la plage existante", `value="11:00"`)
	rec = c.doHX("POST", "/admin/availability", form([]string{"0"}, "11:00", "15:00"))
	mustContain(t, rec, 200, `id="availability-panel"`, "chevauche")

	// Suppression.
	id := regexp.MustCompile(`/admin/availability/([0-9a-f-]{36})/delete`).FindStringSubmatch(page.Body.String())
	if id == nil {
		t.Fatal("bouton de suppression introuvable")
	}
	if rec := c.do("POST", "/admin/availability/"+id[1]+"/delete", url.Values{}); rec.Code != 303 {
		t.Fatalf("suppression: %d", rec.Code)
	}
	if rec := c.do("POST", "/admin/availability/zzz/delete", url.Values{}); rec.Code != 404 {
		t.Errorf("suppression id invalide: %d", rec.Code)
	}
}

func TestAbsencesPage(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)

	mustContain(t, c.do("GET", "/admin/absences", nil), 200, "Aucune absence à venir.", `name="start_date"`)

	rec := c.do("POST", "/admin/absences", url.Values{"start_date": {"2026-11-02"}, "start_time": {"08:00"}})
	mustContain(t, rec, 422, "deux heures", `value="2026-11-02"`)

	rec = c.do("POST", "/admin/absences", url.Values{"start_date": {"2026-11-02"}, "end_date": {"2026-11-04"}, "reason": {"Congrès"}})
	if rec.Code != 303 {
		t.Fatalf("ajout: %d", rec.Code)
	}
	page := c.do("GET", "/admin/absences", nil)
	mustContain(t, page, 200, "Absence ajoutée.", "Du lundi 2 novembre 2026 au mercredi 4 novembre 2026", "Congrès")

	id := regexp.MustCompile(`/admin/absences/([0-9a-f-]{36})/delete`).FindStringSubmatch(page.Body.String())
	if id == nil {
		t.Fatal("bouton de suppression introuvable")
	}
	rec = c.doHX("POST", "/admin/absences/"+id[1]+"/delete", url.Values{})
	mustContain(t, rec, 200, `id="absences-panel"`, "Absence supprimée.", "Aucune absence à venir.")
}
