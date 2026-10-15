package web

import (
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"doctolibre/internal/config"
	"doctolibre/internal/httpx"
)

func newTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	loc, _ := time.LoadLocation("Europe/Paris")
	rd, err := NewRenderer(RendererOptions{SecretKey: strings.Repeat("k", 32), Location: loc})
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	return rd
}

func getPage(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestAllTemplatesParseAndRender(t *testing.T) {
	t.Parallel()
	rd := newTestRenderer(t)
	names := rd.PageNames()
	if len(names) < 2 {
		t.Fatalf("pages trouvées: %v", names)
	}
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			data := any(nil)
			if n == "error" {
				data = ErrorData{Status: 500, Title: "t", Message: "m"}
			}
			if n == "login" {
				data = LoginData{}
			}
			if n == "admin_messages" {
				data = MessagesData{}
			}
			if n == "password_reset" {
				data = PasswordResetData{Token: "t"}
			}
			switch n {
			case "admin_settings":
				data = SettingsPage{}
			case "admin_types":
				data = &TypesPage{}
			case "admin_availability":
				data = &AvailabilityPage{}
			case "admin_absences":
				data = &AbsencesPage{}
			case "public_practice":
				data = PublicPracticeData{}
			case "public_practitioner":
				data = PublicPractitionerData{}
			case "public_book":
				data = GridData{}
			case "public_details":
				data = DetailsData{}
			case "public_verify":
				data = VerifyData{}
			case "public_confirmed":
				data = ConfirmedData{}
			}
			rd.Page(rec, httptest.NewRequest("GET", "/", nil), n, data)
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), `<html lang="fr">`) {
				t.Errorf("page %s: code %d", n, rec.Code)
			}
		})
	}
}

func parseFuncTemplate(rd *Renderer, src string) (*template.Template, error) {
	return template.New("t").Funcs(rd.funcs).Parse(src)
}

func TestHomeRender(t *testing.T) {
	t.Parallel()
	rec := getPage(newHandler(nil), "/")
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	for _, want := range []string{
		"<h1>Prise de rendez-vous en ligne</h1>",
		`<html lang="fr">`,
		`name="viewport"`,
		`<meta name="csrf-token" content="`,
		`hx-headers="`,
		`/static/htmx.min.js?v=`,
		`id="main"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment manquant %q", want)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type %q", ct)
	}
	// CSP : ni script ni style inline, ni ressource externe.
	if regexp.MustCompile(`<script(?:\s[^>]*)?>`).FindAllString(body, -1) != nil {
		for _, s := range regexp.MustCompile(`<script(?:\s[^>]*)?>`).FindAllString(body, -1) {
			if !strings.Contains(s, "src=") {
				t.Errorf("script inline: %s", s)
			}
		}
	}
	if strings.Contains(body, "<style") || regexp.MustCompile(`\sstyle=`).MatchString(body) || strings.Contains(body, "https://") {
		t.Errorf("style inline ou ressource externe dans la page")
	}
}

func TestCSRFTokenExposedToTemplates(t *testing.T) {
	t.Parallel()
	rec := getPage(newHandler(nil), "/")
	var cookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == httpx.CSRFCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("pas de cookie CSRF")
	}
	body := rec.Body.String()
	if !strings.Contains(body, `content="`+cookie+`"`) {
		t.Error("meta csrf-token absent ou différent du cookie")
	}
	if !strings.Contains(body, "X-CSRF-Token") || !strings.Contains(body, cookie) {
		t.Error("hx-headers sans jeton")
	}
}

func TestNotFoundPage(t *testing.T) {
	t.Parallel()
	rec := getPage(newHandler(nil), "/nope")
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "Page introuvable") {
		t.Errorf("code %d body %q", rec.Code, rec.Body.String())
	}
}

func TestPanicRendersStyled500(t *testing.T) {
	t.Parallel()
	rd := newTestRenderer(t)
	h := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		httpx.RequestID(slog.New(slog.NewTextHandler(io.Discard, nil))),
		httpx.RecoverWith(rd.Error))
	rec := getPage(h, "/")
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "Une erreur est survenue") {
		t.Errorf("code %d body %q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Error("le détail de la panic fuite dans la page")
	}
}

func TestStaticAssets(t *testing.T) {
	t.Parallel()
	h := newHandler(nil)
	home := getPage(h, "/").Body.String()
	m := regexp.MustCompile(`/static/app\.css\?v=[0-9a-f]+`).FindString(home)
	if m == "" {
		t.Fatal("URL app.css versionnée absente")
	}
	rec := getPage(h, m)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("code %d cache %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := getPage(h, "/static/app.css"); rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("sans ?v= : cache %q", rec.Header().Get("Cache-Control"))
	}
	for _, p := range []string{"/static/htmx.min.js", "/static/pico.min.css", "/static/app.js"} {
		if rec := getPage(h, p); rec.Code != 200 || rec.Body.Len() == 0 {
			t.Errorf("%s: code %d", p, rec.Code)
		}
	}
	if rec := getPage(h, "/static/"); rec.Code != 404 {
		t.Errorf("listing de répertoire: code %d", rec.Code)
	}
	if rec := getPage(h, "/static/absent.css"); rec.Code != 404 {
		t.Errorf("absent: code %d", rec.Code)
	}
}

func TestFlashRoundTrip(t *testing.T) {
	t.Parallel()
	rd := newTestRenderer(t)
	h := Routes(Deps{Config: config.Config{Env: config.EnvDev}, Renderer: rd,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	set := httptest.NewRecorder()
	rd.SetFlash(set, "success", "Rendez-vous <confirmé>")
	cookies := set.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies: %v", cookies)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `class="flash flash-success"`) || !strings.Contains(body, "Rendez-vous &lt;confirmé&gt;") {
		t.Errorf("flash non rendu/échappé: %q", body)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "flash" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("cookie flash non effacé")
	}

	// Cookie falsifié : ignoré.
	req = httptest.NewRequest("GET", "/", nil)
	c := *cookies[0] //nolint:gosec // test
	c.Value = "e30." + strings.Repeat("A", 43)
	req.AddCookie(&c)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), `class="flash`) {
		t.Error("flash falsifié accepté")
	}
}

func TestPartialAndFuncs(t *testing.T) {
	t.Parallel()
	rd := newTestRenderer(t)
	rec := httptest.NewRecorder()
	rd.Partial(rec, httptest.NewRequest("GET", "/", nil), "form_errors", []string{"Nom requis"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<li>Nom requis</li>") || strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("partial: %d %q", rec.Code, rec.Body.String())
	}

	tests := []struct{ tpl, want string }{
		{`{{formatDate .T}}`, "dimanche 25 octobre 2026"}, // 23:30 UTC le 24 → 00h30 le 25 (CET, DST fini)
		{`{{formatTime .T}}`, "00h30"},
		{`{{formatDate .First}}`, "jeudi 1er octobre 2026"},
		{`{{formatDuration .D45}}`, "45 min"},
		{`{{formatDuration .D60}}`, "1 h"},
		{`{{formatDuration .D90}}`, "1 h 30"},
		{`{{csrf "a\"b"}}`, `<input type="hidden" name="csrf_token" value="a&#34;b">`},
		{`{{with dict "a" 1 "b" "x"}}{{.a}}{{.b}}{{end}}`, "1x"},
	}
	data := map[string]any{
		"T":     time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC), // 24 oct. 22:30 UTC = 25 oct. 00:30 CEST
		"First": time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
		"D45":   45 * time.Minute, "D60": time.Hour, "D90": 90 * time.Minute,
	}
	for _, tt := range tests {
		tpl, err := parseFuncTemplate(rd, tt.tpl)
		if err != nil {
			t.Fatal(err)
		}
		var sb strings.Builder
		if err := tpl.Execute(&sb, data); err != nil {
			t.Fatal(err)
		}
		if sb.String() != tt.want {
			t.Errorf("%s = %q, attendu %q", tt.tpl, sb.String(), tt.want)
		}
	}
}
