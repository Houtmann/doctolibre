package web_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

func posterClient(t *testing.T, role, baseURL string) *client {
	t.Helper()
	db := testutil.DB(t)
	if _, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{
		Email: userEmail, Role: role, PracticeName: "Cabinet Poster", Password: pw,
	}); err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev, BaseURL: baseURL},
		Logger: logger, DB: db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Practice: &service.Practice{Pool: db, Clock: clock},
	})
	c := newClient(t, h, "192.0.2.40")
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	return c
}

func TestPosterShowsURLNameAndQR(t *testing.T) {
	t.Parallel()
	for _, role := range []string{domain.RoleAdmin, domain.RoleSecretary} {
		c := posterClient(t, role, "https://rdv.example.test/")
		rec := c.do("GET", "/admin/poster", nil)
		body := rec.Body.String()
		if rec.Code != 200 {
			t.Fatalf("%s: statut %d", role, rec.Code)
		}
		for _, want := range []string{"Cabinet Poster", "https://rdv.example.test/p/", "<svg", `fill="black"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: manque %q", role, want)
			}
		}
		if dash := c.do("GET", "/admin", nil).Body.String(); !strings.Contains(dash, `href="/admin/poster"`) {
			t.Errorf("%s: lien absent du tableau de bord", role)
		}
	}
}

func TestPosterWithoutBaseURLExplainsWhy(t *testing.T) {
	t.Parallel()
	c := posterClient(t, domain.RoleAdmin, "")
	rec := c.do("GET", "/admin/poster", nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "DOCTOLIBRE_BASE_URL") || strings.Contains(body, "QR code de la page") {
		t.Fatalf("message attendu sans QR: %d\n%s", rec.Code, body)
	}
}
