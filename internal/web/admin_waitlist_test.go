package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"testing"
	"time"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

var cancelActionRe = regexp.MustCompile(`/admin/waitlist/([0-9a-f-]{36})/cancel`)

func TestAdminWaitlistScreen(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: userEmail, Role: domain.RoleAdmin, PracticeName: "Cabinet test", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	typ := testutil.NewAppointmentType(t, db, u.PracticeID)
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: logger, DB: db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Waitlist: &service.Waitlist{Pool: db, Clock: clock, ManageKey: service.NewManageKey(testutil.ManageSecret)},
	})
	c := newClient(t, h, "192.0.2.62")
	if rec := c.do("GET", "/admin/waitlist", nil); rec.Code != 303 {
		t.Fatalf("sans session: %d", rec.Code)
	}
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}

	rec := c.do("GET", "/admin/waitlist", nil)
	mustContain(t, rec, 200, "Aucune inscription pour le moment", "Inscrire un patient", typ.Name, `name="csrf_token"`)

	form := url.Values{
		"first_name": {"Léa"}, "last_name": {"Durand"}, "email": {"lea@example.test"}, "type_id": {typ.ID.String()},
		"earliest": {"2026-10-15"}, "latest": {"2026-10-31"}, "weekdays": {"1", "3"}, "time_from": {"09:00"}, "time_to": {"12:00"},
	}
	bad := url.Values{}
	for k, v := range form {
		bad[k] = v
	}
	bad.Set("latest", "2026-10-01")
	mustContain(t, c.do("POST", "/admin/waitlist", bad), 422, "doit suivre la première", "Durand")

	if rec := c.do("POST", "/admin/waitlist", form); rec.Code != 303 || rec.Header().Get("Location") != "/admin/waitlist" {
		t.Fatalf("ajout: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	rec = c.do("GET", "/admin/waitlist", nil)
	mustContain(t, rec, 200, "Léa Durand", "du 15/10/2026 au 31/10/2026", "mardi, jeudi", "de 9h00 à 12h00", "En attente", "Tous les praticiens", "Retirer")
	m := cancelActionRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("action de retrait absente")
	}
	if rec := c.do("POST", "/admin/waitlist/"+m[1]+"/cancel", url.Values{}); rec.Code != 303 {
		t.Fatalf("retrait: %d", rec.Code)
	}
	mustContain(t, c.do("GET", "/admin/waitlist", nil), 200, "Retirée")
	if rec := c.do("POST", "/admin/waitlist/"+m[1]+"/cancel", url.Values{}); rec.Code != 404 {
		t.Errorf("second retrait: %d, want 404", rec.Code)
	}
}
