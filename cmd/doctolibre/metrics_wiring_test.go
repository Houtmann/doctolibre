package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"doctolibre/internal/metrics"
	"doctolibre/internal/testutil"
)

// TestRegisterDBMetrics : /metrics expose les métriques de l'outbox et du pool.
func TestRegisterDBMetrics(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t)
	pc := testutil.NewPractice(t, db)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `INSERT INTO outbox_message (practice_id, kind, template, recipient, status)
		VALUES ($1, 'email', 't', 'a@example.test', 'failed'), ($1, 'email', 't', 'b@example.test', 'pending')`, pc.ID); err != nil {
		t.Fatal(err)
	}
	m := metrics.New()
	if err := registerDBMetrics(m, db); err != nil {
		t.Fatal(err)
	}
	const token = "jeton-de-metriques-1234" //nolint:gosec // jeton de test
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	m.Handler(token).ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		`doctolibre_outbox_messages{status="failed"} 1`,
		`doctolibre_outbox_messages{status="pending"} 1`,
		"doctolibre_outbox_oldest_pending_age_seconds ",
		"doctolibre_db_pool_max_conns ",
		"doctolibre_db_pool_total_conns ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manque %q", want)
		}
	}
}
