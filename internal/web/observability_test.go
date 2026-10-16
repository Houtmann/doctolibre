package web_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/metrics"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

// syncBuffer : tampon de logs sûr pour les goroutines des handlers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

const obsToken = "jeton-de-metriques-1234" //nolint:gosec // jeton de test

// TestBookingJourneyNoPIIAndMetrics : le parcours de réservation complet ne laisse
// aucune donnée patient (ni code, ni jeton de gestion) dans les logs, et le compteur
// de rendez-vous créés n'est incrémenté qu'une fois, après le commit.
func TestBookingJourneyNoPIIAndMetrics(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t)
	pc := testutil.NewPractice(t, db)
	pr := testutil.NewPractitioner(t, db, pc.ID)
	typ := testutil.NewAppointmentType(t, db, pc.ID)
	if _, err := sqlcgen.New(db).CreateAvailabilityRule(context.Background(), sqlcgen.CreateAvailabilityRuleParams{
		PractitionerID: pr.ID, Weekday: 1,
		StartTime: pgtype.Time{Microseconds: 9 * time.Hour.Microseconds(), Valid: true},
		EndTime:   pgtype.Time{Microseconds: 12 * time.Hour.Microseconds(), Valid: true},
		ValidFrom: pgtype.Date{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	otp := &otpCapture{}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	m := metrics.New()
	logs := &syncBuffer{}
	svc := &service.Booking{Pool: db, Clock: clock, OTP: otp,
		OnCreated: func(source string) { m.AppointmentsCreated.WithLabelValues(source).Inc() }}
	h := web.Routes(web.Deps{
		Config:  config.Config{Env: config.EnvDev, MetricsToken: obsToken},
		Logger:  slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		DB:      db,
		Booking: svc,
		Metrics: m,
	})
	e := &publicEnv{h: h, db: db, otp: otp, pc: pc, pr: pr, typ: typ, svc: svc, clock: clock}
	c := newClient(t, h, "192.0.2.77")
	const start = "2026-10-20T07:00:00Z"
	detailsURL := e.base() + "/book/details?type=" + typ.ID.String() + "&start=" + url.QueryEscape(start)

	c.do("GET", e.base(), nil)
	c.do("GET", detailsURL, nil)
	rec := c.do("POST", detailsURL, detailsForm(typ.ID.String(), start))
	m1 := challengeRe.FindStringSubmatch(rec.Body.String())
	if m1 == nil {
		t.Fatalf("pas de challenge: %d", rec.Code)
	}
	code := otp.code("+33611111111")
	confirm := detailsForm(typ.ID.String(), start)
	confirm.Set("challenge_id", m1[1])
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	confirm.Set("code", wrong)
	c.do("POST", e.base()+"/book/confirm", confirm)
	if got := scrape(h); strings.Contains(got, "doctolibre_appointments_created_total{") {
		t.Fatal("compteur incrémenté sans rendez-vous créé")
	}
	confirm.Set("code", code)
	rec = c.do("POST", e.base()+"/book/confirm", confirm)
	manage := rec.Header().Get("Location")
	if rec.Code != 303 || !strings.HasPrefix(manage, "/r/") {
		t.Fatalf("confirmation: %d %q", rec.Code, manage)
	}
	c.do("GET", manage, nil)
	c.do("GET", manage+"/ics", nil)

	if got := scrape(h); !strings.Contains(got, `doctolibre_appointments_created_total{source="online"} 1`) {
		t.Errorf("compteur attendu à 1, métriques:\n%s", got)
	}
	out := logs.String()
	if out == "" {
		t.Fatal("aucun log produit : test sans valeur")
	}
	token := strings.TrimPrefix(manage, "/r/")
	for _, pii := range []string{"Alice", "Durand", "alice@example.test", "alice%40example.test", "06 11 11 11 11", "0611111111", "+33611111111", code, token} {
		if strings.Contains(out, pii) {
			t.Errorf("donnée sensible %q trouvée dans les logs", pii)
		}
	}
}

func scrape(h http.Handler) string {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+obsToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String()
}

func TestAdminStatusPage(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t)
	ctx := context.Background()
	_, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: "admin@example.test", Role: domain.RoleAdmin, PracticeName: "Cabinet état", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: "secretaire@example.test", Role: domain.RoleSecretary, PracticeName: "Cabinet état", Password: pw}); err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(&syncBuffer{}, nil))
	started := clock.Now().Add(-26 * time.Hour)
	st := &service.Status{Pool: db, Clock: clock, Version: "v-test", StartedAt: started}
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: logger, DB: db,
		Auth:   &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Status: st,
	})

	anon := newClient(t, h, "192.0.2.1")
	if rec := anon.do("GET", "/admin/status", nil); rec.Code != 303 {
		t.Errorf("sans session: %d", rec.Code)
	}
	sec := newClient(t, h, "192.0.2.2")
	sec.login("secretaire@example.test", pw, "")
	if rec := sec.do("GET", "/admin/status", nil); rec.Code != 403 {
		t.Errorf("non-admin: %d, attendu 403", rec.Code)
	}
	adm := newClient(t, h, "192.0.2.3")
	adm.login("admin@example.test", pw, "")
	rec := adm.do("GET", "/admin/status", nil)
	if rec.Code != 200 {
		t.Fatalf("admin: %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"v-test", "1 j 2 h", "Version des migrations", "Taille de la base", "Messages en échec", "Suivi non configuré"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment %q absent", want)
		}
	}
}
