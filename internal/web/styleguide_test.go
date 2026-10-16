package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"doctolibre/internal/config"
)

func styleguideGet(t *testing.T, env string) *httptest.ResponseRecorder {
	t.Helper()
	return styleguidePathGet(t, env, "/dev/styleguide")
}

func styleguidePathGet(t *testing.T, env, path string) *httptest.ResponseRecorder {
	t.Helper()
	h := Routes(Deps{
		Config: config.Config{Env: env},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestStyleguideDev(t *testing.T) {
	t.Parallel()
	rec := styleguideGet(t, config.EnvDev)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, veut 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"dl-alert--success", "dl-alert--warning", "st-no_show", "st-cancelled", "form-errors",
		"sw-st-done", "booking-summary", "patient-results",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment %q absent", want)
		}
	}
	if strings.Contains(body, " style=") || strings.Contains(body, "<script>") {
		t.Error("style ou script inline interdit par la CSP")
	}
}

func TestStyleguideNotInProd(t *testing.T) {
	t.Parallel()
	if rec := styleguideGet(t, config.EnvProd); rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, veut 404", rec.Code)
	}
}

func TestStyleguideV2Dev(t *testing.T) {
	t.Parallel()
	rec := styleguidePathGet(t, config.EnvDev, "/dev/styleguide/v2")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, veut 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"dl-btn", "dl-alert--error", "dl-appt st-no_show", `data-theme="dark"`,
		"dl-cal__head", "dl-cal-list", "dl-dialog", "dl-table", "dl-field is-invalid", "tokens.css", "components.css",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment %q absent", want)
		}
	}
	if strings.Contains(body, " style=") || strings.Contains(body, "<script>") {
		t.Error("style ou script inline interdit par la CSP")
	}
}

func TestStyleguideV2NotInProd(t *testing.T) {
	t.Parallel()
	if rec := styleguidePathGet(t, config.EnvProd, "/dev/styleguide/v2"); rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, veut 404", rec.Code)
	}
}
