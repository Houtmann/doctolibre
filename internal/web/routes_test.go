package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"doctolibre/internal/config"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func newHandler(db Pinger) http.Handler {
	return Routes(Deps{
		Config: config.Config{Env: config.EnvDev},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:     db,
	})
}

func TestRoutes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		db     Pinger
		method string
		path   string
		code   int
		body   string
	}{
		{"healthz sans DB", nil, "GET", "/healthz", 200, "ok"},
		{"healthz DB ok", fakePinger{}, "GET", "/healthz", 200, "ok"},
		{"healthz DB ko", fakePinger{errors.New("down")}, "GET", "/healthz", 503, ""},
		{"accueil", nil, "GET", "/", 200, "Doctolibre"},
		{"inconnue", nil, "GET", "/nope", 404, ""},
		{"POST sans CSRF", nil, "POST", "/healthz", 403, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			newHandler(tt.db).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.code {
				t.Errorf("code %d, attendu %d", rec.Code, tt.code)
			}
			if !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("body %q", rec.Body.String())
			}
			// Headers de sécurité et request id sur toutes les réponses.
			h := rec.Header()
			if h.Get("Content-Security-Policy") == "" || h.Get("X-Frame-Options") != "DENY" ||
				h.Get("Referrer-Policy") != "same-origin" || h.Get("X-Request-ID") == "" {
				t.Errorf("headers manquants: %v", h)
			}
		})
	}
}
