package web_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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

var frozenInstant = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func frozenNow() time.Time { return frozenInstant }

const limitsMetricsToken = "limits-metrics-token-0123456789" //nolint:gosec // jeton de test

// TestAdminUserRateLimit : un utilisateur connecté qui dépasse admin-user (5/s, rafale 100)
// reçoit 429 ; un autre utilisateur, même depuis la même IP, n'est pas touché. Le refus
// est un fragment pour htmx, porte Retry-After et alimente la métrique.
func TestAdminUserRateLimit(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t)
	ctx := context.Background()
	for _, email := range []string{"a@example.test", "b@example.test"} {
		if _, err := auth.CreateUser(ctx, db, auth.CreateUserParams{
			Email: email, Role: domain.RoleAdmin, PracticeName: "Cabinet " + email, Password: pw,
		}); err != nil {
			t.Fatal(err)
		}
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := metrics.New()
	h := web.Routes(web.Deps{
		Config:  config.Config{Env: config.EnvDev, MetricsToken: limitsMetricsToken},
		Logger:  logger,
		DB:      db,
		Metrics: m,
		Auth:    &auth.Service{Pool: db, Clock: clock, Logger: logger},

		LimiterNow: frozenNow,
	})
	const ip = "198.51.100.77"
	a, b := newClient(t, h, ip), newClient(t, h, ip)
	if rec := a.login("a@example.test", pw, ""); rec.Code != 303 {
		t.Fatalf("login a: %d", rec.Code)
	}
	if rec := b.login("b@example.test", pw, ""); rec.Code != 303 {
		t.Fatalf("login b: %d", rec.Code)
	}

	for i := 1; i <= 100; i++ {
		if code := a.do("GET", "/admin", nil).Code; code != 200 {
			t.Fatalf("requête %d: %d, la rafale de 100 doit passer", i, code)
		}
	}
	if code := a.do("GET", "/admin", nil).Code; code != http.StatusTooManyRequests {
		t.Fatalf("101e requête de l'utilisateur a: %d, attendu 429", code)
	}
	if rec := b.do("GET", "/admin", nil); rec.Code != 200 {
		t.Errorf("autre utilisateur, même IP: %d, attendu 200", rec.Code)
	}

	rec := a.do("GET", "/admin", nil)
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Errorf("refus sans Retry-After: %d %v", rec.Code, rec.Header())
	}
	if !strings.Contains(rec.Body.String(), "Trop de requêtes") {
		t.Errorf("page 429 sans message: %q", rec.Body.String())
	}
	hx := a.doWith("GET", "/admin", "HX-Request", "true")
	if hx.Code != 429 || !strings.Contains(hx.Body.String(), `class="alert alert-warning"`) || strings.Contains(hx.Body.String(), "<html") {
		t.Errorf("htmx doit recevoir un fragment d'alerte: %d %q", hx.Code, hx.Body.String())
	}

	// Métrique par limiteur, sans IP ni clé.
	mreq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mreq.Header.Set("Authorization", "Bearer "+limitsMetricsToken)
	mrec := httptest.NewRecorder()
	h.ServeHTTP(mrec, mreq)
	body := mrec.Body.String()
	if !strings.Contains(body, `doctolibre_rate_limited_total{limiter="admin-user"}`) {
		t.Errorf("métrique absente:\n%s", body)
	}
	if strings.Contains(body, ip) {
		t.Error("l'IP ne doit jamais apparaître dans les métriques")
	}
}

// TestAdminIPRateLimit : le flood non authentifié (cookie invalide) est coupé par admin-ip,
// avant tout accès DB, par préfixe /64 pour IPv6.
func TestAdminIPRateLimit(t *testing.T) {
	t.Parallel()
	h, _ := newAdmin(t)
	// Deux adresses du même /64 partagent le seau.
	hit := func(addr string) int {
		c := newClient(t, h, addr)
		c.cookies["session"] = &http.Cookie{Name: "session", Value: "jeton-invalide"} //nolint:gosec // cookie de requête simulé
		return c.do("GET", "/admin", nil).Code
	}
	got429 := false
	for i := range 210 {
		if hit(fmt.Sprintf("[2001:db8:0:1::%x]", i+1)) == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("200 requêtes non authentifiées d'un même /64 devraient être limitées")
	}
	if c := hit("[2001:db8:0:2::1]"); c == http.StatusTooManyRequests {
		t.Error("un autre /64 ne doit pas être limité")
	}
	if c := hit("203.0.113.5"); c == http.StatusTooManyRequests {
		t.Error("une IPv4 n'est pas touchée")
	}
}

// TestAdminHeavyRateLimit : les routes coûteuses ont un seau par utilisateur partagé
// (1 req / 2 s, rafale 10), même sur des routes différentes.
func TestAdminHeavyRateLimit(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t)
	if _, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{
		Email: userEmail, Role: domain.RoleAdmin, PracticeName: "Cabinet", Password: pw,
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
		Patients: &service.Patients{Pool: db, Clock: clock},

		LimiterNow: frozenNow,
	})
	c := newClient(t, h, "192.0.2.10")
	if rec := c.login(userEmail, pw, ""); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	id := uuid.NewString()
	for i := range 10 {
		path := "/admin/patients/" + id + "/export.csv"
		if i%2 == 1 {
			path = "/admin/patients/" + id + "/export.json"
		}
		if rec := c.do("GET", path, nil); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("export %d refusé trop tôt", i+1)
		}
	}
	if rec := c.do("GET", "/admin/patients/"+id+"/export.csv", nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("11e export: %d, attendu 429 (seau partagé entre les deux routes)", rec.Code)
	}
	// Une route ordinaire n'est pas concernée par admin-heavy.
	if rec := c.do("GET", "/admin/patients", nil); rec.Code != 200 {
		t.Errorf("liste des patients: %d", rec.Code)
	}
}

// TestPublicPOSTsShareOneBucket : POST details et POST confirm décomptent le même seau
// par IP (burst 10) ; les recherches (GET) ont le leur.
func TestPublicPOSTsShareOneBucket(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.201")
	tid := e.typ.ID.String()
	const start = "2026-10-20T07:00:00Z"
	for i := range 10 {
		path := "/book/details"
		if i%2 == 1 {
			path = "/book/confirm"
		}
		// (le POST details peut répondre 429 « trop de codes » : seul le message du limiteur compte)
		rec := c.do("POST", e.base()+path+"?type="+tid+"&start="+url.QueryEscape(start), detailsForm(tid, start))
		if strings.Contains(rec.Body.String(), "Trop de requêtes") {
			t.Fatalf("POST %d refusé trop tôt", i+1)
		}
	}
	for _, path := range []string{"/book/details", "/book/confirm"} {
		rec := c.do("POST", e.base()+path, detailsForm(tid, start))
		if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Trop de requêtes") {
			t.Errorf("POST %s après 10 POST: %d, attendu 429 du limiteur", path, rec.Code)
		}
	}
	if rec := c.do("GET", e.base()+"/book?type="+tid, nil); rec.Code != 200 {
		t.Errorf("la grille a son propre seau: %d", rec.Code)
	}
	// Pages d'accueil : seau « browse » (rafale 30) distinct.
	for i := range 30 {
		if rec := c.do("GET", "/p/"+e.pc.Slug, nil); rec.Code != 200 {
			t.Fatalf("accueil %d: %d", i+1, rec.Code)
		}
	}
	if rec := c.do("GET", "/p/"+e.pc.Slug, nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("31e accueil: %d, attendu 429", rec.Code)
	}
}

// bookViaOTP mène une réservation complète depuis une IP et un téléphone dédiés ; wrong
// ajoute d'abord une confirmation avec un mauvais code. Retourne le code HTTP de la confirmation finale.
func bookViaOTP(t *testing.T, e *publicEnv, base string, i int, start string, wrong bool) int {
	t.Helper()
	c := newClient(t, e.h, fmt.Sprintf("198.18.%d.%d", i/200, i%200+1))
	tid := e.typ.ID.String()
	phone := fmt.Sprintf("06 12 34 56 %02d", i)
	form := func() url.Values {
		f := detailsForm(tid, start)
		f.Set("phone", phone)
		f.Set("email", fmt.Sprintf("p%d@example.test", i))
		return f
	}
	rec := c.do("POST", base+"/book/details?type="+tid+"&start="+url.QueryEscape(start), form())
	m := challengeRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("réservation %d : pas de challenge (%d)", i, rec.Code)
	}
	code := e.otp.code(fmt.Sprintf("+336123456%02d", i))
	confirm := form()
	confirm.Set("challenge_id", m[1])
	if wrong {
		confirm.Set("code", "000001")
		if code == "000001" {
			confirm.Set("code", "000002")
		}
		if rec := c.do("POST", base+"/book/confirm", confirm); rec.Code != 422 {
			t.Fatalf("mauvais code: %d, attendu 422", rec.Code)
		}
	}
	confirm.Set("code", code)
	return c.do("POST", base+"/book/confirm", confirm).Code
}

// TestOnlineBookingQuotaPerPractitioner : 30 réservations aboutes par heure et par praticien ;
// la 31e est refusée (429), l'autre praticien n'est pas touché, et un code faux ne consomme pas.
func TestOnlineBookingQuotaPerPractitioner(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	// Second praticien du même cabinet, mêmes horaires.
	pr2 := testutil.NewPractitioner(t, e.db, e.pc.ID)
	if _, err := sqlcgen.New(e.db).CreateAvailabilityRule(context.Background(), sqlcgen.CreateAvailabilityRuleParams{
		PractitionerID: pr2.ID, Weekday: 1,
		StartTime: pgtype.Time{Microseconds: 9 * time.Hour.Microseconds(), Valid: true},
		EndTime:   pgtype.Time{Microseconds: 12 * time.Hour.Microseconds(), Valid: true},
		ValidFrom: pgtype.Date{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	// 30 créneaux : 5 mardis × 6 demi-heures de 9 h à 12 h (Paris, CET/CEST).
	var slots []string
	for _, day := range []string{"2026-10-20", "2026-10-27", "2026-11-03", "2026-11-10", "2026-11-17"} {
		for _, hm := range []string{"08:00", "08:30", "09:00", "09:30", "10:00", "10:30"} {
			slots = append(slots, day+"T"+hm+":00Z")
		}
	}
	// 2026-10-27 et suivants : heure d'hiver (CET) depuis le 25/10, 9 h Paris = 08:00Z.
	// Pour 2026-10-20 (CEST) 9 h Paris = 07:00Z : on corrige les 6 premiers.
	copy(slots[:6], []string{"2026-10-20T07:00:00Z", "2026-10-20T07:30:00Z", "2026-10-20T08:00:00Z", "2026-10-20T08:30:00Z", "2026-10-20T09:00:00Z", "2026-10-20T09:30:00Z"})

	for i, s := range slots {
		if code := bookViaOTP(t, e, e.base(), i, s, i == 3); code != http.StatusSeeOther {
			t.Fatalf("réservation %d/30 : %d, attendue en 303 (un code faux ne doit pas consommer le quota)", i+1, code)
		}
	}
	if n := e.appointments(t); n != 30 {
		t.Fatalf("%d rendez-vous, attendu 30", n)
	}
	if code := bookViaOTP(t, e, e.base(), 30, "2026-11-24T08:00:00Z", false); code != http.StatusTooManyRequests {
		t.Errorf("31e réservation : %d, attendu 429", code)
	}
	if n := e.appointments(t); n != 30 {
		t.Errorf("la 31e réservation a créé un rendez-vous (%d)", n)
	}
	// L'autre praticien n'est pas affecté.
	other := "/p/" + e.pc.Slug + "/" + pr2.Slug
	if code := bookViaOTP(t, e, other, 31, "2026-10-20T07:00:00Z", false); code != http.StatusSeeOther {
		t.Errorf("autre praticien : %d, attendu 303", code)
	}
}
