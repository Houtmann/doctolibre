package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

const (
	pw        = "un-mot-de-passe-solide" //nolint:gosec // mot de passe de test
	userEmail = "kine@example.test"
	csrfValue = "0123456789abcdef0123456789abcdef0123456789"
)

func newAdmin(t *testing.T) (http.Handler, *testutil.FixedClock) {
	t.Helper()
	db := testutil.DB(t)
	if _, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{
		Email: userEmail, Role: domain.RoleAdmin, PracticeName: "Cabinet test", Password: pw,
	}); err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev},
		Logger: logger,
		DB:     db,
		Auth:   &auth.Service{Pool: db, Clock: clock, Logger: logger},
	})
	return h, clock
}

// client simule un navigateur minimal : cookies conservés, CSRF automatique.
type client struct {
	t       *testing.T
	h       http.Handler
	cookies map[string]*http.Cookie
	addr    string
}

func newClient(t *testing.T, h http.Handler, addr string) *client {
	return &client{t: t, h: h, cookies: map[string]*http.Cookie{}, addr: addr}
}

func (c *client) do(method, target string, form url.Values) *httptest.ResponseRecorder {
	c.t.Helper()
	var body io.Reader
	if form != nil {
		if tok, ok := c.cookies["csrf_token"]; ok {
			form.Set("csrf_token", tok.Value)
		} else {
			form.Set("csrf_token", csrfValue)
			c.cookies["csrf_token"] = &http.Cookie{Name: "csrf_token", Value: csrfValue} //nolint:gosec // cookie de requête simulé
		}
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	req.RemoteAddr = c.addr + ":4321"
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.MaxAge < 0 {
			delete(c.cookies, ck.Name)
		} else {
			c.cookies[ck.Name] = ck
		}
	}
	return rec
}

func (c *client) login(email, password, next string) *httptest.ResponseRecorder {
	return c.do("POST", "/admin/login", url.Values{"email": {email}, "password": {password}, "next": {next}})
}

func TestAdminRequiresLogin(t *testing.T) {
	t.Parallel()
	h, _ := newAdmin(t)
	c := newClient(t, h, "192.0.2.1")
	rec := c.do("GET", "/admin", nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/login?next=%2Fadmin" {
		t.Fatalf("sans session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := c.do("POST", "/admin/logout", url.Values{}); rec.Code != 303 || rec.Header().Get("Location") != "/admin/login" {
		t.Errorf("logout sans session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = c.do("GET", "/admin/login?next=%2Fadmin", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="password"`) || !strings.Contains(rec.Body.String(), `name="csrf_token"`) {
		t.Errorf("formulaire de connexion: %d", rec.Code)
	}
}

func TestLoginLogoutFlow(t *testing.T) {
	t.Parallel()
	h, clock := newAdmin(t)
	c := newClient(t, h, "192.0.2.1")

	rec := c.login(userEmail, pw, "/admin")
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin" {
		t.Fatalf("login: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	ck := c.cookies["session"]
	if ck == nil || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" || ck.Secure {
		t.Fatalf("cookie de session inattendu (dev): %+v", ck)
	}
	token := ck.Value
	if rec := c.do("GET", "/admin", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Tableau de bord") ||
		!strings.Contains(rec.Body.String(), "/admin/logout") {
		t.Fatalf("tableau de bord: %d", rec.Code)
	}
	// Déjà connecté : le formulaire redirige.
	if rec := c.do("GET", "/admin/login", nil); rec.Code != 303 || rec.Header().Get("Location") != "/admin" {
		t.Errorf("login déjà connecté: %d", rec.Code)
	}

	rec = c.do("POST", "/admin/logout", url.Values{})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("logout: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if c.cookies["session"] != nil {
		t.Error("cookie non effacé au logout")
	}
	// Le token volé/rejoué est invalide côté serveur.
	replay := newClient(t, h, "192.0.2.2")
	replay.cookies["session"] = &http.Cookie{Name: "session", Value: token} //nolint:gosec // cookie de requête simulé
	if rec := replay.do("GET", "/admin", nil); rec.Code != 303 {
		t.Errorf("token révoqué accepté: %d", rec.Code)
	}

	// Session expirée → déconnecté.
	c.login(userEmail, pw, "")
	clock.Advance(13 * time.Hour)
	if rec := c.do("GET", "/admin", nil); rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/login") {
		t.Errorf("session expirée acceptée: %d", rec.Code)
	}
}

func TestLoginRotatesToken(t *testing.T) {
	t.Parallel()
	h, _ := newAdmin(t)
	c := newClient(t, h, "192.0.2.1")
	c.login(userEmail, pw, "")
	first := c.cookies["session"].Value
	c.login(userEmail, pw, "")
	second := c.cookies["session"].Value
	if first == second {
		t.Fatal("token identique après nouveau login")
	}
	old := newClient(t, h, "192.0.2.2")
	old.cookies["session"] = &http.Cookie{Name: "session", Value: first} //nolint:gosec // cookie de requête simulé
	if rec := old.do("GET", "/admin", nil); rec.Code != 303 {
		t.Errorf("ancienne session encore valide: %d", rec.Code)
	}
}

func TestLoginFailureIsGeneric(t *testing.T) {
	t.Parallel()
	h, _ := newAdmin(t)
	bad := newClient(t, h, "192.0.2.1").login(userEmail, "mauvais-mot-de-passe", "")
	unknown := newClient(t, h, "192.0.2.2").login("inconnu@example.test", "mauvais-mot-de-passe", "")
	for _, rec := range []*httptest.ResponseRecorder{bad, unknown} {
		if rec.Code != 401 || !strings.Contains(rec.Body.String(), "Adresse email ou mot de passe incorrect.") {
			t.Fatalf("échec: %d", rec.Code)
		}
		if hasCookie(rec, "session") {
			t.Error("cookie de session posé après un échec")
		}
	}
	if strings.Contains(bad.Body.String(), "mauvais-mot-de-passe") {
		t.Error("le mot de passe est renvoyé dans la page")
	}
}

func hasCookie(rec *httptest.ResponseRecorder, name string) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name && c.MaxAge >= 0 && c.Value != "" {
			return true
		}
	}
	return false
}

func TestLoginOpenRedirectRefused(t *testing.T) {
	t.Parallel()
	h, _ := newAdmin(t)
	for _, next := range []string{"//evil.example", "https://evil.example", "/\\evil.example", "javascript:alert(1)"} {
		c := newClient(t, h, "192.0.2.1")
		rec := c.login(userEmail, pw, next)
		if rec.Code != 303 || rec.Header().Get("Location") != "/admin" {
			t.Errorf("next=%q → %d %q", next, rec.Code, rec.Header().Get("Location"))
		}
	}
	c := newClient(t, h, "192.0.2.1")
	if rec := c.login(userEmail, pw, "/admin?x=1"); rec.Header().Get("Location") != "/admin?x=1" {
		t.Errorf("next relatif refusé: %q", rec.Header().Get("Location"))
	}
	// Le formulaire ne réinjecte pas un next hostile.
	rec := newClient(t, h, "192.0.2.3").do("GET", "/admin/login?next=//evil.example", nil)
	if strings.Contains(rec.Body.String(), "evil.example") {
		t.Error("next hostile réinjecté dans le formulaire")
	}
}

func TestLoginRateLimit(t *testing.T) {
	t.Parallel()
	h, _ := newAdmin(t)

	// Par IP : la 6e tentative depuis la même IP est refusée, même avec le bon mot de passe.
	c := newClient(t, h, "198.51.100.1")
	for i := 0; i < 5; i++ {
		if rec := c.login("a"+string(rune('a'+i))+"@example.test", "x", ""); rec.Code != 401 {
			t.Fatalf("tentative %d: %d", i+1, rec.Code)
		}
	}
	if rec := c.login(userEmail, pw, ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("6e tentative par IP: %d", rec.Code)
	}

	// Par email : IP différentes, même cible.
	for i := 0; i < 5; i++ {
		ci := newClient(t, h, "203.0.113."+string(rune('1'+i)))
		if rec := ci.login(userEmail, "mauvais", ""); rec.Code != 401 {
			t.Fatalf("tentative %d: %d", i+1, rec.Code)
		}
	}
	other := newClient(t, h, "203.0.113.99")
	if rec := other.login(" "+strings.ToUpper(userEmail), pw, ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("6e tentative par email (casse/espaces ignorés): %d", rec.Code)
	}
	// Un autre email depuis une autre IP n'est pas affecté.
	if rec := newClient(t, h, "203.0.113.98").login("autre@example.test", "x", ""); rec.Code != 401 {
		t.Errorf("autre email: %d", rec.Code)
	}
}

func TestLoginRequiresCSRF(t *testing.T) {
	t.Parallel()
	h, _ := newAdmin(t)
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(url.Values{"email": {userEmail}, "password": {pw}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Errorf("POST sans CSRF: %d", rec.Code)
	}
}
