package web

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"doctolibre/internal/config"
	"doctolibre/internal/metrics"
)

const testToken = "0123456789abcdef-token"

func metricsHandler(token string) http.Handler {
	return Routes(Deps{
		Config:  config.Config{Env: config.EnvDev, MetricsToken: token},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics: metrics.New(),
	})
}

func get(h http.Handler, path, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMetricsAccess(t *testing.T) {
	t.Parallel()
	h := metricsHandler(testToken)
	tests := []struct {
		name, auth string
		code       int
	}{
		{"sans token", "", http.StatusUnauthorized},
		{"mauvais token", "Bearer nope", http.StatusUnauthorized},
		{"mauvais schéma", testToken, http.StatusUnauthorized},
		{"bon token", "Bearer " + testToken, http.StatusOK},
	}
	for _, tt := range tests {
		if got := get(h, "/metrics", tt.auth).Code; got != tt.code {
			t.Errorf("%s: code %d, attendu %d", tt.name, got, tt.code)
		}
	}
}

func TestMetricsDisabledByDefault(t *testing.T) {
	t.Parallel()
	// Pas de token : route absente même avec un registre.
	if got := get(metricsHandler(""), "/metrics", "Bearer x").Code; got != http.StatusNotFound {
		t.Errorf("code %d, attendu 404", got)
	}
	if got := get(newHandler(nil), "/metrics", "").Code; got != http.StatusNotFound {
		t.Errorf("code %d, attendu 404", got)
	}
}

func TestMetricsRouteLabelIsPattern(t *testing.T) {
	t.Parallel()
	h := metricsHandler(testToken)
	get(h, "/healthz", "")
	get(h, "/inconnu/12345?email=a@b.fr", "")
	body := get(h, "/metrics", "Bearer "+testToken).Body.String()
	if !strings.Contains(body, `route="GET /healthz"`) {
		t.Errorf("label route pattern absent:\n%s", body)
	}
	// Depuis T004, le 404 stylé est servi par le fallback « GET / » : c'est son pattern qui sert de label.
	if !strings.Contains(body, `route="GET /"`) {
		t.Error("route inconnue devrait être étiquetée « GET / » (fallback 404)")
	}
	if strings.Contains(body, "12345") || strings.Contains(body, "a@b.fr") {
		t.Error("chemin brut ou query dans les métriques")
	}
}

type fakeOutbox struct{}

func (fakeOutbox) OutboxStats(context.Context) (metrics.OutboxStats, error) {
	return metrics.OutboxStats{ByStatus: map[string]int64{"failed": 2}, OldestPending: 90 * time.Second}, nil
}

func TestMetricsExtensionPoints(t *testing.T) {
	t.Parallel()
	m := metrics.New()
	if err := m.RegisterOutbox(fakeOutbox{}); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterPool(func() metrics.PoolStats { return metrics.PoolStats{Max: 10} }); err != nil {
		t.Fatal(err)
	}
	m.AppointmentsCreated.WithLabelValues("online").Inc()
	h := Routes(Deps{Config: config.Config{MetricsToken: testToken}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Metrics: m})
	body := get(h, "/metrics", "Bearer "+testToken).Body.String()
	for _, want := range []string{
		`doctolibre_outbox_messages{status="failed"} 2`,
		"doctolibre_outbox_oldest_pending_age_seconds 90",
		"doctolibre_db_pool_max_conns 10",
		`doctolibre_appointments_created_total{source="online"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manque %q", want)
		}
	}
}

// TestNoPIIInLogs : une requête portant des données patient (query, body,
// headers, cookie) ne doit laisser aucune trace de ces données dans les logs.
// À étendre avec le parcours de réservation complet quand il existera (T014).
func TestNoPIIInLogs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := Routes(Deps{
		Config:  config.Config{Env: config.EnvDev, MetricsToken: testToken},
		Logger:  slog.New(slog.NewJSONHandler(&buf, nil)),
		Metrics: metrics.New(),
	})
	pii := []string{"Jeanne Dupont", "jeanne.dupont@example.org", "0612345678", "otp-654321", "secret-session-token"}
	for _, path := range []string{"/healthz", "/", "/nope", "/metrics"} {
		req := httptest.NewRequest(http.MethodPost,
			path+"?nom=Jeanne+Dupont&email=jeanne.dupont%40example.org&tel=0612345678",
			strings.NewReader("nom=Jeanne+Dupont&email=jeanne.dupont%40example.org&otp=otp-654321"))
		req.Header.Set("Authorization", "Bearer secret-session-token")
		req.Header.Set("Cookie", "session=secret-session-token")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	logs := buf.String()
	if logs == "" {
		t.Fatal("aucun log produit : test sans valeur")
	}
	for _, p := range pii {
		if strings.Contains(logs, p) || strings.Contains(logs, strings.ReplaceAll(p, " ", "+")) {
			t.Errorf("PII %q trouvée dans les logs", p)
		}
	}
}
