package web_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

type statsWebEnv struct {
	admin, sec, p1, p2 *client
	id1, id2           uuid.UUID
}

func newStatsWebEnv(t *testing.T) statsWebEnv {
	t.Helper()
	db := testutil.DB(t)
	ctx := context.Background()
	mk := func(email, role string) auth.User {
		u, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: email, Role: role, PracticeName: "Cabinet stats", Password: pw})
		if err != nil {
			t.Fatal(err)
		}
		return auth.User{ID: u.ID, PracticeID: u.PracticeID, Email: u.Email, Role: u.Role}
	}
	adm := mk("admin@example.test", domain.RoleAdmin)
	sec := mk("secretaire@example.test", domain.RoleSecretary)
	u1 := mk("p1@example.test", domain.RolePractitioner)
	u2 := mk("p2@example.test", domain.RolePractitioner)
	clock := testutil.NewFixedClock(time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &service.Practice{Pool: db, Clock: clock}
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: logger, DB: db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Practice: svc,
		Stats:    &service.Stats{Pool: db, Clock: clock},
	})
	e := statsWebEnv{}
	login := func(addr, email string) *client {
		c := newClient(t, h, addr)
		if rec := c.login(email, pw, "/admin"); rec.Code != 303 {
			t.Fatalf("login %s: %d", email, rec.Code)
		}
		return c
	}
	e.admin, e.sec = login("192.0.2.51", adm.Email), login("192.0.2.52", sec.Email)
	e.p1, e.p2 = login("192.0.2.53", u1.Email), login("192.0.2.54", u2.Email)
	prac := func(u auth.User, first string) uuid.UUID {
		pr, err := svc.Practitioner(ctx, service.Actor{UserID: u.ID, PracticeID: u.PracticeID, Role: domain.RolePractitioner})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `UPDATE practitioner SET first_name = $2 WHERE id = $1`, pr.ID, first); err != nil {
			t.Fatal(err)
		}
		return pr.ID
	}
	e.id1, e.id2 = prac(u1, "Alice"), prac(u2, "Bruno")
	typ := testutil.NewAppointmentType(t, db, adm.PracticeID)
	if _, err := db.Exec(ctx, `UPDATE appointment_type SET name = 'Séance rééducation' WHERE id = $1`, typ.ID); err != nil {
		t.Fatal(err)
	}
	patient := testutil.NewPatient(t, db, adm.PracticeID)
	for i, row := range []struct {
		prac   uuid.UUID
		status string
	}{{e.id1, "done"}, {e.id1, "done"}, {e.id1, "no_show"}, {e.id2, "done"}, {e.id2, "cancelled"}} {
		_, err := db.Exec(ctx, `INSERT INTO appointment (practice_id, practitioner_id, patient_id, appointment_type_id, during, during_blocked, status, source)
			VALUES ($1, $2, $3, $4, tstzrange($5::timestamptz, $5::timestamptz + interval '30 minutes'), tstzrange($5::timestamptz, $5::timestamptz + interval '30 minutes'), $6, 'staff')`,
			adm.PracticeID, row.prac, patient.ID, typ.ID, time.Date(2026, 10, 14, 7+i, 0, 0, 0, time.UTC), row.status)
		if err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func TestStatsPageAndCSVAccess(t *testing.T) {
	t.Parallel()
	e := newStatsWebEnv(t)
	q := "?from=2026-10-01&to=2026-10-31"

	// Administrateur : tous les praticiens, filtre, lien vers l'export ; l'accueil renvoie vers la page.
	mustContain(t, e.admin.do("GET", "/admin/stats"+q, nil), 200, "Alice", "Bruno", "Séance rééducation", `href="/admin/stats.csv?from=2026-10-01&amp;to=2026-10-31"`, `name="practitioner"`, "Total général : 3 faites, 1 absents, 1 annulées")
	mustContain(t, e.admin.do("GET", "/admin/stats", nil), 200, `value="2026-10-01"`, `value="2026-10-31"`) // mois précédent par défaut
	mustContain(t, e.admin.do("GET", "/admin/stats"+q+"&practitioner="+e.id2.String(), nil), 200, "Bruno", "Total général : 1 faites, 0 absents, 1 annulées")
	mustContain(t, e.admin.do("GET", "/admin", nil), 200, `href="/admin/stats"`)

	// Praticien : lui-même, sans sélecteur ; pas les chiffres d'un confrère.
	own := e.p1.do("GET", "/admin/stats"+q, nil)
	mustContain(t, own, 200, "Alice", "Total général : 2 faites, 1 absents, 0 annulées")
	if strings.Contains(own.Body.String(), "Bruno") || strings.Contains(own.Body.String(), `name="practitioner"`) {
		t.Errorf("le praticien voit un confrère ou le sélecteur")
	}
	if code := e.p1.do("GET", "/admin/stats"+q+"&practitioner="+e.id2.String(), nil).Code; code != 403 {
		t.Errorf("praticien vise un confrère: %d", code)
	}
	if code := e.p1.do("GET", "/admin/stats.csv"+q+"&practitioner="+e.id2.String(), nil).Code; code != 403 {
		t.Errorf("export vers un confrère: %d", code)
	}

	// Secrétariat : 403 sur la page et l'export.
	for _, path := range []string{"/admin/stats", "/admin/stats.csv"} {
		if code := e.sec.do("GET", path+q, nil).Code; code != 403 {
			t.Errorf("secrétariat %s: %d", path, code)
		}
	}
	if code := e.admin.do("GET", "/admin/stats"+q+"&practitioner="+uuid.NewString(), nil).Code; code != 404 {
		t.Errorf("praticien inconnu: %d", code)
	}
	if code := e.admin.do("GET", "/admin/stats?practitioner=nope", nil).Code; code != 404 {
		t.Errorf("identifiant invalide: %d", code)
	}
}

func TestStatsPageInvalidPeriod(t *testing.T) {
	t.Parallel()
	e := newStatsWebEnv(t)
	mustContain(t, e.admin.do("GET", "/admin/stats?from=2026-10-31&to=2026-10-01", nil), 422, "La date de fin précède la date de début.", `value="2026-10-31"`, `name="practitioner"`)
	mustContain(t, e.admin.do("GET", "/admin/stats?from=2025-01-01&to=2026-10-01", nil), 422, "ne peut pas dépasser 366 jours")
	if code := e.admin.do("GET", "/admin/stats.csv?from=bad&to=2026-10-01", nil).Code; code != 400 {
		t.Errorf("export période invalide: %d", code)
	}
}

func TestStatsCSVDownload(t *testing.T) {
	t.Parallel()
	e := newStatsWebEnv(t)
	rec := e.admin.do("GET", "/admin/stats.csv?from=2026-10-01&to=2026-10-31", nil)
	if rec.Code != 200 {
		t.Fatalf("export: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="seances-2026-10-01_2026-10-31.csv"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\xEF\xBB\xBFPraticien titulaire;Réalisé par;Rôle;Motif;Lieu;Faites;Absents;Annulées;Dont à domicile\r\n") {
		t.Errorf("en-têtes ou BOM: %q", body)
	}
	if !strings.Contains(body, "Alice p1;Alice p1;titulaire;Séance rééducation;cabinet;2;1;0;0\r\n") {
		t.Errorf("ligne attendue absente:\n%s", body)
	}
	if strings.Contains(body, "Alex") || strings.Contains(body, "Dupont") {
		t.Errorf("donnée patient dans le CSV:\n%s", body)
	}
}
