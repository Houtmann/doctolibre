package web_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
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

func newImportEnv(t *testing.T, role string) patEnv {
	t.Helper()
	db := testutil.DB(t)
	u, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{Email: userEmail, Role: role, PracticeName: "Cabinet test", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &service.Patients{Pool: db, Clock: clock}
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: logger, DB: db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Patients: svc, PatientImport: &service.PatientImport{Pool: db, Clock: clock},
	})
	c := newClient(t, h, "192.0.2.31")
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	return patEnv{c: c, db: db, actor: service.Actor{UserID: u.ID, PracticeID: u.PracticeID}, svc: svc}
}

// postMultipart envoie un formulaire multipart avec le cookie CSRF du client.
func postMultipart(c *client, target string, fields map[string]string, file []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if file != nil {
		fw, _ := mw.CreateFormFile("file", "patients.csv")
		_, _ = fw.Write(file)
	}
	_ = mw.WriteField("csrf_token", c.cookies["csrf_token"].Value)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.Close()
	req := httptest.NewRequest("POST", target, &buf)
	req.RemoteAddr = c.addr + ":4321"
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func (e patEnv) countPatients(t *testing.T) (total, opted int) {
	t.Helper()
	if err := e.db.QueryRow(context.Background(),
		`SELECT count(*), count(*) FILTER (WHERE sms_opt_in OR email_opt_in) FROM patient WHERE practice_id = $1`, e.actor.PracticeID).Scan(&total, &opted); err != nil {
		t.Fatal(err)
	}
	return total, opted
}

var payloadRE = regexp.MustCompile(`name="payload" value="([^"]*)"`)

func TestPatientImportFlowForEveryRole(t *testing.T) {
	// Fichier Windows-1252 : « Hélène » en \xe9 ; une ligne en doublon de la base, une invalide.
	csv := []byte("Nom;Pr\xe9nom;T\xe9l\xe9phone;Email\nMartin;H\xe9l\xe8ne;06 12 34 56 78;\nDurand;Paul;;paul@example.test\nExistant;Zo\xe9;07 11 22 33 44;\nSans;Contact;;\n")
	for _, role := range []string{domain.RoleSecretary, domain.RolePractitioner, domain.RoleAdmin} {
		t.Run(role, func(t *testing.T) {
			e := newImportEnv(t, role)
			e.newPatient(t, "Zoé", "Existant", "+33711223344")

			page := e.c.do("GET", "/admin/patients/import", nil)
			if page.Code != 200 || !strings.Contains(page.Body.String(), `enctype="multipart/form-data"`) || !strings.Contains(page.Body.String(), `type="file"`) {
				t.Fatalf("formulaire : %d", page.Code)
			}
			if !strings.Contains(e.c.do("GET", "/admin/patients", nil).Body.String(), "/admin/patients/import") {
				t.Error("lien d'import absent de la liste")
			}

			prev := postMultipart(e.c, "/admin/patients/import", nil, csv)
			body := prev.Body.String()
			if prev.Code != 200 || !strings.Contains(body, "MARTIN Hélène") || !strings.Contains(body, "33612345678") ||
				!strings.Contains(body, "Ce téléphone existe déjà") || !strings.Contains(body, "Ni téléphone ni email") ||
				!strings.Contains(body, `name="notify_opt_in"`) || !strings.Contains(body, "Importer 2 patient") {
				t.Fatalf("aperçu : %d\n%s", prev.Code, body)
			}
			if total, _ := e.countPatients(t); total != 1 {
				t.Fatalf("l'aperçu a écrit en base : %d", total)
			}
			m := payloadRE.FindStringSubmatch(body)
			if m == nil {
				t.Fatal("payload absent")
			}

			rec := postMultipart(e.c, "/admin/patients/import", map[string]string{"confirm": "1", "payload": m[1]}, nil)
			if rec.Code != 303 || rec.Header().Get("Location") != "/admin/patients" {
				t.Fatalf("confirmation : %d %s", rec.Code, rec.Body.String())
			}
			if total, opted := e.countPatients(t); total != 3 || opted != 1 { // 1 = la fiche préexistante
				t.Errorf("total=%d opt-in=%d (case non cochée : aucun opt-in)", total, opted)
			}
		})
	}
}

func TestPatientImportConfirmWithConsent(t *testing.T) {
	e := newImportEnv(t, domain.RoleSecretary)
	payload := base64.StdEncoding.EncodeToString([]byte("nom,prenom,mail\nA,B,ab@example.test\n"))
	rec := postMultipart(e.c, "/admin/patients/import", map[string]string{"confirm": "1", "payload": payload, "notify_opt_in": "1"}, nil)
	if rec.Code != 303 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if total, opted := e.countPatients(t); total != 1 || opted != 1 {
		t.Errorf("total=%d opt-in=%d", total, opted)
	}
}

func TestPatientImportLimits(t *testing.T) {
	e := newImportEnv(t, domain.RoleSecretary)
	post := func(file []byte) *httptest.ResponseRecorder {
		return postMultipart(e.c, "/admin/patients/import", nil, file)
	}

	// ~1,5 Mio : passe le CSRF et l'upload (ligne invalide car adresse trop longue, mais l'aperçu s'affiche).
	big := []byte("nom;prenom;email;adresse\nA;B;a@example.test;" + strings.Repeat("x", 3<<19) + "\n")
	if rec := post(big); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Invalide") {
		t.Errorf("1,5 Mio : %d", rec.Code)
	}
	// Plus de 2 Mo mais moins de 4 Mio : refus explicite.
	tooBig := append([]byte("nom;prenom;email\n"), bytes.Repeat([]byte("a"), 3<<20)...)
	if rec := post(tooBig); rec.Code != 422 || !strings.Contains(rec.Body.String(), "dépasse 2 Mo") {
		t.Errorf("3 Mio : %d", rec.Code)
	}
	// Plus de 5000 lignes.
	var many strings.Builder
	many.WriteString("nom;prenom;email\n")
	for i := 0; i < 5001; i++ {
		many.WriteString("A;B;a@example.test\n")
	}
	if rec := post([]byte(many.String())); rec.Code != 422 || !strings.Contains(rec.Body.String(), "5000 lignes") {
		t.Errorf("5001 lignes : %d", rec.Code)
	}
	// Pas de fichier, en-tête sans prénom.
	if rec := post(nil); rec.Code != 422 || !strings.Contains(rec.Body.String(), "Choisissez un fichier") {
		t.Errorf("sans fichier : %d", rec.Code)
	}
	if rec := post([]byte("nom;email\nA;a@example.test\n")); rec.Code != 422 || !strings.Contains(rec.Body.String(), "Prénom") {
		t.Errorf("sans prénom : %d", rec.Code)
	}
	// Payload de confirmation trop gros ou corrompu.
	if rec := postMultipart(e.c, "/admin/patients/import", map[string]string{"confirm": "1", "payload": "!!!"}, nil); rec.Code != 422 {
		t.Errorf("payload corrompu : %d", rec.Code)
	}
	if total, _ := e.countPatients(t); total != 0 {
		t.Errorf("rien ne devait être importé : %d", total)
	}
}

func TestPatientImportRequiresLoginAndCSRF(t *testing.T) {
	e := newImportEnv(t, domain.RoleSecretary)
	anon := newClient(t, e.c.h, "192.0.2.32")
	if rec := anon.do("GET", "/admin/patients/import", nil); rec.Code != http.StatusSeeOther {
		t.Errorf("sans session : %d", rec.Code)
	}
	// Multipart sans jeton CSRF valide.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("csrf_token", "faux")
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/admin/patients/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for _, ck := range e.c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	e.c.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("CSRF faux : %d", rec.Code)
	}
}
