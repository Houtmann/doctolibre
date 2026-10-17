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
	for _, p := range []string{"/admin/settings", "/admin/profile", "/admin/appointment-types", "/admin/availability", "/admin/absences"} {
		if rec := c.do("GET", p, nil); rec.Code != 403 {
			t.Errorf("GET %s secrétaire: %d", p, rec.Code)
		}
	}
}

func TestSettingsPage(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)

	rec := c.do("GET", "/admin/settings", nil)
	mustContain(t, rec, 200, `value="Cabinet test"`, `name="booking_notice_hours" type="number" inputmode="numeric" min="0" max="720" value="2"`)
	for _, f := range []string{`name="slug"`, `name="first_name"`, `name="color"`, `/admin/settings/feed`} {
		if strings.Contains(rec.Body.String(), f) {
			t.Errorf("le champ du profil %s est sur la page du cabinet", f)
		}
	}

	// Invalide : statut 422, erreurs affichées, saisie conservée.
	rec = c.do("POST", "/admin/settings", url.Values{
		"name": {""}, "booking_notice_hours": {"abc"}, "cancel_notice_hours": {"24"}, "horizon_days": {"60"}, "phone": {"0102030405"},
	})
	mustContain(t, rec, 422, "Le nom du cabinet est requis.", "délai minimal de réservation", `value="0102030405"`, `role="alert"`)

	// Valide : redirection 303 puis valeurs relues.
	rec = c.do("POST", "/admin/settings", url.Values{
		"name": {"Cabinet Étoile"}, "address": {"1 rue de la Paix"}, "phone": {"01 02 03 04 05"},
		"booking_notice_hours": {"6"}, "cancel_notice_hours": {"48"}, "horizon_days": {"45"},
	})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/settings" {
		t.Fatalf("POST valide: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	mustContain(t, c.do("GET", "/admin/settings", nil), 200, "Paramètres du cabinet enregistrés.", `value="Cabinet Étoile"`, `value="45"`)
}

func TestProfilePage(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)

	rec := c.do("GET", "/admin/profile", nil)
	mustContain(t, rec, 200, `name="slug"`, `for="first_name"`, `for="color"`)
	for _, f := range []string{`name="name"`, `name="booking_notice_hours"`, `name="home_visit_zones"`} {
		if strings.Contains(rec.Body.String(), f) {
			t.Errorf("le champ du cabinet %s est sur la page du profil", f)
		}
	}

	rec = c.do("POST", "/admin/profile", url.Values{"first_name": {"Camille"}, "last_name": {""}, "slug": {"camille"}, "color": {"#aa3366"}})
	mustContain(t, rec, 422, "Le nom du praticien est requis.", `value="Camille"`, `role="alert"`)

	rec = c.do("POST", "/admin/profile", url.Values{
		"first_name": {"Camille"}, "last_name": {"Martin"}, "title": {"Kiné"}, "bio": {"Bio"}, "slug": {"camille-martin"}, "color": {"#aa3366"},
	})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/profile" {
		t.Fatalf("POST valide: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	mustContain(t, c.do("GET", "/admin/profile", nil), 200, "Profil enregistré.", `value="camille-martin"`, `name="color" type="color" value="#aa3366"`, `for="color"`)

	// Couleur invalide : 422 et rien n'est écrit.
	for _, bad := range []string{"rouge", "#12345", "url(x)", ""} {
		rec = c.do("POST", "/admin/profile", url.Values{
			"first_name": {"Autre"}, "last_name": {"Martin"}, "slug": {"autre-slug"}, "color": {bad},
		})
		mustContain(t, rec, 422, "La couleur doit être au format #rrggbb.")
	}
	mustContain(t, c.do("GET", "/admin/profile", nil), 200, `value="Camille"`, `value="camille-martin"`, `value="#aa3366"`)
}

// Un praticien non admin règle son profil, jamais le cabinet : tout champ du cabinet
// posté est refusé (403), rien n'est écrit, et l'ancien lien mène au profil.
func TestPractitionerProfileNotPracticeSettings(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RolePractitioner)

	profile := c.do("GET", "/admin/profile", nil)
	mustContain(t, profile, 200, `name="slug"`, `<a href="/admin/profile"`)
	if strings.Contains(profile.Body.String(), `href="/admin/settings"`) {
		t.Error("lien vers les réglages du cabinet proposé à un praticien")
	}
	rec := c.do("GET", "/admin/settings", nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/profile" {
		t.Errorf("GET /admin/settings praticien: %d %q, attendu redirection vers le profil", rec.Code, rec.Header().Get("Location"))
	}

	ok := url.Values{"first_name": {"Léa"}, "last_name": {"Durand"}, "slug": {"lea-durand"}, "color": {"#112233"}}
	if rec := c.do("POST", "/admin/profile", ok); rec.Code != 303 {
		t.Fatalf("profil: %d", rec.Code)
	}
	// Champ du cabinet posté sur le profil.
	bad := url.Values{"first_name": {"Léa"}, "last_name": {"Durand"}, "slug": {"lea-durand"}, "color": {"#112233"}, "name": {"Piraté"}}
	if rec := c.do("POST", "/admin/profile", bad); rec.Code != 403 {
		t.Errorf("champ cabinet sur le profil: %d, attendu 403", rec.Code)
	}
	// Réglages du cabinet, notifications et fournisseurs : tous refusés.
	cabinet := url.Values{"name": {"Piraté"}, "booking_notice_hours": {"1"}, "cancel_notice_hours": {"1"}, "horizon_days": {"10"}}
	for _, p := range []string{"/admin/settings", "/admin/settings/notifications"} {
		if rec := c.do("POST", p, cabinet); rec.Code != 403 {
			t.Errorf("POST %s praticien: %d, attendu 403", p, rec.Code)
		}
	}
	for _, p := range []string{"/admin/settings/notifications"} {
		if rec := c.do("GET", p, nil); rec.Code != 403 {
			t.Errorf("GET %s praticien: %d, attendu 403", p, rec.Code)
		}
	}
}

func TestSecretaryProfileForbidden(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleSecretary)
	if rec := c.do("GET", "/admin/profile", nil); rec.Code != 403 {
		t.Errorf("GET /admin/profile secrétaire: %d", rec.Code)
	}
	if rec := c.do("POST", "/admin/profile", url.Values{"first_name": {"A"}, "last_name": {"B"}, "slug": {"ab"}, "color": {"#112233"}}); rec.Code != 403 {
		t.Errorf("POST /admin/profile secrétaire: %d", rec.Code)
	}
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

func TestResourcesPanel(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)

	mustContain(t, c.do("GET", "/admin/appointment-types", nil), 200, "Ressources partagées", `name="resource"`, `for="type-resource"`)
	mustContain(t, c.do("POST", "/admin/resources", url.Values{"resource_name": {""}}), 422, "Le nom de la ressource est requis.")
	if rec := c.do("POST", "/admin/resources", url.Values{"resource_name": {"Box 1"}}); rec.Code != 303 {
		t.Fatalf("création: %d", rec.Code)
	}
	rec := c.do("GET", "/admin/appointment-types", nil)
	mustContain(t, rec, 200, "Ressource créée.", "Box 1")
	rid := regexp.MustCompile(`<option value="([0-9a-f-]{36})"[^>]*>Box 1`).FindStringSubmatch(rec.Body.String())
	tid := regexp.MustCompile(`/admin/appointment-types/([0-9a-f-]{36})"`).FindStringSubmatch(rec.Body.String())
	if rid == nil || tid == nil {
		t.Fatal("ressource ou motif introuvable")
	}
	// Liaison au motif, visible dans la liste et dans le formulaire d'édition.
	if rec := c.do("POST", "/admin/appointment-types/"+tid[1], url.Values{"name": {"Bilan"}, "duration": {"40"}, "resource": {rid[1]}}); rec.Code != 303 {
		t.Fatalf("liaison: %d", rec.Code)
	}
	mustContain(t, c.do("GET", "/admin/appointment-types/"+tid[1], nil), 200, `value="`+rid[1]+`" selected`)
	if rec := c.do("POST", "/admin/resources/"+rid[1]+"/active", url.Values{"active": {"0"}}); rec.Code != 303 {
		t.Fatalf("désactivation: %d", rec.Code)
	}
	mustContain(t, c.do("GET", "/admin/appointment-types", nil), 200, "(désactivée)", "Ressource désactivée.")
	// Ressource inconnue : refusée.
	mustContain(t, c.do("POST", "/admin/appointment-types/"+tid[1], url.Values{"name": {"Bilan"}, "duration": {"40"}, "resource": {"00000000-0000-0000-0000-000000000001"}}), 422, "Ressource inconnue.")
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

func TestAdminNavMarksCurrentPage(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)

	rec := c.do("GET", "/admin/settings", nil)
	mustContain(t, rec, 200, `<a href="/admin/settings" class="menu-active" aria-current="page">Paramètres du cabinet</a>`, `<a href="/admin/profile">Mon profil</a>`, `class="menu w-full`, `drawer-side`, `data-theme-toggle`)
	if n := strings.Count(rec.Body.String(), `aria-current="page"`); n != 1 {
		t.Fatalf("aria-current=page: %d occurrences, attendu 1", n)
	}
	// Le tableau de bord n'a pas d'entrée courante.
	if rec := c.do("GET", "/admin", nil); strings.Contains(rec.Body.String(), `aria-current="page"`) {
		t.Fatal("aria-current inattendu sur le tableau de bord")
	}
}
