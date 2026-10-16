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
	h := Routes(Deps{
		Config: config.Config{Env: env},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/styleguide", nil))
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
		"flash-success", "flash-warning", "st-no_show", "st-cancelled", "form-errors",
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
