package web_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

type privWebEnv struct {
	patEnv
	slug string
}

func newPrivacyWebEnv(t *testing.T, role string) privWebEnv {
	t.Helper()
	db := testutil.DB(t)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: userEmail, Role: role, PracticeName: "Cabinet test", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &service.Patients{Pool: db, Clock: clock}
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: logger, DB: db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Patients: svc,
		Privacy:  &service.Privacy{Pool: db, Clock: clock},
		Audit:    &service.Audit{Pool: db, Clock: clock},
	})
	c := newClient(t, h, "192.0.2.31")
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	var slug string
	if err := db.QueryRow(ctx, `SELECT slug FROM practice WHERE id = $1`, u.PracticeID).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	return privWebEnv{patEnv: patEnv{c: c, db: db, actor: service.Actor{UserID: u.ID, PracticeID: u.PracticeID}, svc: svc}, slug: slug}
}

func auditCount(t *testing.T, db *pgxpool.Pool, where string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE `+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPrivacyEndpointsRefuseSecretary(t *testing.T) {
	e := newPrivacyWebEnv(t, domain.RoleSecretary)
	p := e.newPatient(t, "Alex", "Dupont", "+33611111111")
	id := p.ID.String()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/admin/patients/" + id + "/export.json"},
		{"GET", "/admin/patients/" + id + "/export.csv"},
		{"POST", "/admin/patients/" + id + "/delete"},
		{"GET", "/admin/privacy"},
		{"GET", "/admin/audit"},
	} {
		if rec := e.c.do(tc.method, tc.path, url.Values{"confirm": {"1"}}); rec.Code != 403 {
			t.Errorf("%s %s = %d, attendu 403", tc.method, tc.path, rec.Code)
		}
	}
	page := e.c.do("GET", "/admin/patients/"+id, nil).Body.String()
	if strings.Contains(page, "export.json") || strings.Contains(page, "Supprimer la fiche") {
		t.Error("export et suppression ne doivent pas être proposés au secrétariat")
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM patient WHERE id = $1 AND anonymized_at IS NULL AND deleted_at IS NULL`, p.ID).Scan(&n); err != nil || n != 1 {
		t.Error("la fiche ne devait pas changer")
	}
	if auditCount(t, e.db, `action = 'patient_view' AND entity_id = $1`, p.ID) != 1 {
		t.Error("la consultation de la fiche par le secrétariat doit être journalisée")
	}
}

func TestPatientViewExportAndDeleteAreAudited(t *testing.T) {
	e := newPrivacyWebEnv(t, domain.RolePractitioner)
	p := e.newPatient(t, "Alex", "Dupont", "+33611111111")
	id := p.ID.String()

	e.c.do("GET", "/admin/patients/"+id, nil)
	e.c.do("GET", "/admin/patients/"+id, nil)
	if n := auditCount(t, e.db, `action = 'patient_view' AND entity_id = $1 AND user_id = $2`, p.ID, e.actor.UserID); n != 2 {
		t.Errorf("consultations journalisées = %d, attendu 2", n)
	}

	rec := e.c.do("GET", "/admin/patients/"+id+"/export.json", nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("export JSON: %d, %v", rec.Code, rec.Header())
	}
	var exp service.PatientExport
	if err := json.Unmarshal(rec.Body.Bytes(), &exp); err != nil || exp.Patient.LastName != "Dupont" || exp.Patient.Phone != "+33611111111" {
		t.Errorf("JSON exporté: %v, %+v", err, exp.Patient)
	}
	rec = e.c.do("GET", "/admin/patients/"+id+"/export.csv", nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") || !strings.Contains(rec.Body.String(), "Dupont") {
		t.Fatalf("export CSV: %d", rec.Code)
	}
	if auditCount(t, e.db, `action = 'patient.exported' AND entity_id = $1`, p.ID) != 2 {
		t.Error("exports non journalisés")
	}
	if rec := e.c.do("GET", "/admin/patients/"+uuid.NewString()+"/export.json", nil); rec.Code != 404 {
		t.Errorf("export d'une fiche inconnue = %d", rec.Code)
	}

	// Suppression : la case de confirmation est obligatoire.
	if rec := e.c.do("POST", "/admin/patients/"+id+"/delete", url.Values{}); rec.Code != 422 {
		t.Errorf("suppression sans confirmation = %d, attendu 422", rec.Code)
	}
	rec = e.c.do("POST", "/admin/patients/"+id+"/delete", url.Values{"confirm": {"1"}})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/patients" {
		t.Fatalf("suppression = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if auditCount(t, e.db, `action = 'patient.deleted' AND entity_id = $1`, p.ID) != 1 {
		t.Error("suppression non journalisée")
	}
	if rec := e.c.do("GET", "/admin/patients/"+id, nil); rec.Code != 404 {
		t.Errorf("fiche supprimée = %d, attendu 404", rec.Code)
	}
	// Aucune donnée personnelle dans le journal.
	if n := auditCount(t, e.db, `meta::text ILIKE '%dupont%' OR meta::text LIKE '%+336%' OR meta::text ILIKE '%example.test%'`); n != 0 {
		t.Errorf("%d lignes d'audit contiennent des données personnelles", n)
	}
}

func TestAdminPrivacySettingsAndAuditScreen(t *testing.T) {
	e := newPrivacyWebEnv(t, domain.RoleAdmin)
	page := e.c.do("GET", "/admin/privacy", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "retention_outbox_days") {
		t.Fatalf("GET /admin/privacy = %d", page.Code)
	}
	bad := e.c.do("POST", "/admin/privacy", url.Values{"retention_outbox_days": {"abc"}, "retention_patient_years": {"3"}, "retention_audit_days": {"365"}})
	if bad.Code != 422 {
		t.Errorf("durée invalide = %d, attendu 422", bad.Code)
	}
	tooLow := e.c.do("POST", "/admin/privacy", url.Values{"retention_outbox_days": {"0"}, "retention_patient_years": {"3"}, "retention_audit_days": {"365"}})
	if tooLow.Code != 422 {
		t.Errorf("durée hors limites = %d, attendu 422", tooLow.Code)
	}
	ok := e.c.do("POST", "/admin/privacy", url.Values{
		"retention_outbox_days": {"30"}, "retention_patient_years": {"5"}, "retention_audit_days": {"400"},
		"patient_purge_enabled": {"1"}, "privacy_contact": {"dpo@cabinet.example"},
	})
	if ok.Code != 303 {
		t.Fatalf("enregistrement = %d", ok.Code)
	}
	if body := e.c.do("GET", "/admin/privacy", nil).Body.String(); !strings.Contains(body, `value="30"`) || !strings.Contains(body, "dpo@cabinet.example") {
		t.Error("réglages non relus")
	}

	audit := e.c.do("GET", "/admin/audit", nil)
	body := audit.Body.String()
	if audit.Code != 200 || !strings.Contains(body, "privacy.settings_updated") || !strings.Contains(body, "login_success") {
		t.Errorf("/admin/audit = %d, journal incomplet", audit.Code)
	}
	if filtered := e.c.do("GET", "/admin/audit?action=login_success", nil).Body.String(); strings.Contains(filtered, "privacy.settings_updated") {
		t.Error("filtre par action sans effet")
	}

	// Page publique d'information, sans connexion.
	notice := e.c.do("GET", "/p/"+e.slug+"/privacy", nil)
	nb := notice.Body.String()
	if notice.Code != 200 || !strings.Contains(nb, "Cabinet test") || !strings.Contains(nb, "30 jours") || !strings.Contains(nb, "5 ans") || !strings.Contains(nb, "dpo@cabinet.example") {
		t.Errorf("page d'information = %d", notice.Code)
	}
	if rec := e.c.do("GET", "/p/inconnu/privacy", nil); rec.Code != 404 {
		t.Errorf("cabinet inconnu = %d", rec.Code)
	}
}
