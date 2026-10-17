package httpx

import (
	"bytes"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func ok(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }

func TestLoggerMasksSecretTokens(t *testing.T) {
	t.Parallel()
	const secret = "s3cr3t-T0ken_abc"
	tests := []struct {
		name, path, wantPath string
	}{
		{"flux ICS", "/ics/" + secret + ".ics", `"path":"/ics/…"`},
		{"lien patient", "/r/" + secret + "/cancel", `"path":"/r/…"`},
		{"préfixe seul", "/r/", `"path":"/r/…"`},
		{"chemin ordinaire", "/admin/calendar", `"path":"/admin/calendar"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			h := Chain(http.HandlerFunc(ok), RequestID(logger), Logger())
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tt.path, nil))
			out := buf.String()
			if strings.Contains(out, secret) {
				t.Errorf("jeton dans les logs : %s", out)
			}
			if !strings.Contains(out, tt.wantPath) {
				t.Errorf("chemin attendu %s dans %s", tt.wantPath, out)
			}
		})
	}
}

func TestRequestIDAndLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	var seen string
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
		ok(w, r)
	}), RequestID(logger), Logger())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x?token=secret&nom=Dupont", nil)
	req.Header.Set(RequestIDHeader, "abc-123")
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get(RequestIDHeader); got != "abc-123" || seen != "abc-123" {
		t.Errorf("request id header=%q ctx=%q", got, seen)
	}
	out := buf.String()
	if !strings.Contains(out, `"request_id":"abc-123"`) || !strings.Contains(out, `"path":"/x"`) {
		t.Errorf("log inattendu: %s", out)
	}
	if strings.Contains(out, "secret") || strings.Contains(out, "Dupont") {
		t.Errorf("la query string fuite dans les logs: %s", out)
	}

	// id invalide → régénéré
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "bad id\n")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get(RequestIDHeader); got == "" || got == "bad id\n" {
		t.Errorf("id invalide non remplacé: %q", got)
	}
}

func TestRecover(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		RequestID(logger), Recover())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code %d", rec.Code)
	}
	if !strings.Contains(buf.String(), "boom") || !strings.Contains(buf.String(), "stack") {
		t.Errorf("panic/stack non journalisées: %s", buf.String())
	}
}

func TestSecureHeaders(t *testing.T) {
	t.Parallel()
	for _, prod := range []bool{false, true} {
		rec := httptest.NewRecorder()
		SecureHeaders(prod)(http.HandlerFunc(ok)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		h := rec.Header()
		if !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'self'") ||
			h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") != "same-origin" {
			t.Errorf("headers manquants: %v", h)
		}
		if hsts := h.Get("Strict-Transport-Security") != ""; hsts != prod {
			t.Errorf("prod=%v HSTS présent=%v", prod, hsts)
		}
	}
}

func TestCSRF(t *testing.T) {
	t.Parallel()
	h := CSRF(false)(http.HandlerFunc(ok))

	// GET pose le cookie.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != CSRFCookie || !cookies[0].HttpOnly {
		t.Fatalf("cookie csrf attendu: %v", cookies)
	}
	token := cookies[0].Value

	post := func(setup func(*http.Request)) int {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		setup(req)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	withCookie := func(r *http.Request) { r.AddCookie(&http.Cookie{Name: CSRFCookie, Value: token}) } //nolint:gosec // test

	if c := post(func(*http.Request) {}); c != http.StatusForbidden {
		t.Errorf("POST sans cookie ni jeton: %d", c)
	}
	if c := post(withCookie); c != http.StatusForbidden {
		t.Errorf("POST sans jeton: %d", c)
	}
	if c := post(func(r *http.Request) { withCookie(r); r.Header.Set(CSRFHeader, "faux") }); c != http.StatusForbidden {
		t.Errorf("POST jeton faux: %d", c)
	}
	if c := post(func(r *http.Request) { withCookie(r); r.Header.Set(CSRFHeader, token) }); c != http.StatusOK {
		t.Errorf("POST header valide: %d", c)
	}
	if c := post(func(r *http.Request) {
		withCookie(r)
		r.Body = io.NopCloser(strings.NewReader(url.Values{CSRFFormField: {token}}.Encode()))
	}); c != http.StatusOK {
		t.Errorf("POST champ de formulaire valide: %d", c)
	}
}

func TestRespond(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("HX-Request", "true")
	if !IsHTMX(req) {
		t.Error("IsHTMX")
	}
	rec := httptest.NewRecorder()
	Redirect(rec, req, "/ailleurs")
	if rec.Header().Get("HX-Redirect") != "/ailleurs" {
		t.Errorf("HX-Redirect: %v", rec.Header())
	}
	rec = httptest.NewRecorder()
	Redirect(rec, httptest.NewRequest(http.MethodPost, "/", nil), "/ailleurs")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ailleurs" {
		t.Errorf("redirect: %d %v", rec.Code, rec.Header())
	}
}

func TestCSRFBodyLimits(t *testing.T) {
	t.Parallel()
	h := CSRF(false)(http.HandlerFunc(ok))
	const token = "0123456789abcdef0123456789abcdef0123456789"

	multipart := func(fileSize int) *http.Request {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", "patients.csv")
		_, _ = fw.Write(bytes.Repeat([]byte("a"), fileSize))
		_ = mw.WriteField(CSRFFormField, token) // après le fichier : il faut tout lire
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		return req
	}
	urlencoded := func(padding int) *http.Request {
		body := "pad=" + strings.Repeat("a", padding) + "&" + CSRFFormField + "=" + token
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req
	}
	tests := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"multipart de 1,5 Mio accepté", multipart(3 << 19), http.StatusOK},
		{"multipart de 3 Mio accepté", multipart(3 << 20), http.StatusOK},
		{"multipart au-delà de la limite refusé", multipart(MaxMultipartLen), http.StatusForbidden},
		{"urlencoded de 1,5 Mio toujours refusé", urlencoded(3 << 19), http.StatusForbidden},
		{"urlencoded court accepté", urlencoded(10), http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.req.AddCookie(&http.Cookie{Name: CSRFCookie, Value: token}) //nolint:gosec // test
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tt.req)
			if rec.Code != tt.want {
				t.Errorf("statut %d, attendu %d", rec.Code, tt.want)
			}
		})
	}
}
