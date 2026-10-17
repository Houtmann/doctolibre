package web_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/notify"
	"doctolibre/internal/secretbox"
	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

type mailSink struct{ to []string }

func (m *mailSink) Send(_ context.Context, e notify.Email) (string, error) {
	m.to = append(m.to, e.To)
	return "ref", nil
}

// newTransportAdmin monte le handler complet avec la page des fournisseurs et un faux sender SMTP.
func newTransportAdmin(t *testing.T, role string) (*client, *pgxpool.Pool, *mailSink) {
	t.Helper()
	db := testutil.DB(t)
	if _, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{
		Email: userEmail, Role: role, PracticeName: "Cabinet test", Password: pw,
	}); err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	box, err := secretbox.New(testutil.ManageSecret)
	if err != nil {
		t.Fatal(err)
	}
	sink := &mailSink{}
	ts := &notify.Transports{
		Pool: db, Box: box, Clock: clock, Logger: logger,
		Env:      config.Config{Email: config.EmailConfig{Driver: config.EmailDriverLog, LogDir: t.TempDir()}, SMS: config.SMSConfig{Driver: config.SMSDriverLog}},
		NewEmail: func(config.EmailConfig, bool) (notify.EmailSender, error) { return sink, nil },
	}
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: logger, DB: db,
		Auth:            &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Practice:        &service.Practice{Pool: db, Clock: clock},
		NotifyTransport: service.NewNotifyTransport(db, clock, ts, logger),
	})
	c := newClient(t, h, "192.0.2.10")
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	return c, db, sink
}

const transportPath = "/admin/settings/notifications/transport"

func TestTransportPageSecretsNeverRendered(t *testing.T) {
	t.Parallel()
	c, db, sink := newTransportAdmin(t, domain.RoleAdmin)
	const secret = "Sup3r-Secret-Pw"

	rec := c.do("GET", transportPath, nil)
	mustContain(t, rec, 200, "Fournisseurs d'email et de SMS", `autocomplete="new-password"`, `type="password"`, "chiffrés", "Fournisseur du serveur (<code>log</code>)")

	form := url.Values{
		"mode": {"custom"}, "f_smtp_host": {"smtp.example.org"}, "f_smtp_from": {"Cabinet <rdv@example.org>"},
		"f_smtp_user": {"utilisateur"}, "x_smtp_password": {secret},
	}
	if rec := c.do("POST", transportPath+"/email", form); rec.Code != 303 {
		t.Fatalf("POST valide : %d\n%s", rec.Code, rec.Body)
	}
	rec = c.do("GET", transportPath, nil)
	mustContain(t, rec, 200, "Fournisseur enregistré", "smtp.example.org", "défini")
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("le mot de passe est réaffiché")
	}
	var raw []byte
	if err := db.QueryRow(context.Background(), `SELECT secrets FROM practice_notify_transport WHERE kind = 'email'`).Scan(&raw); err != nil || len(raw) == 0 || bytes.Contains(raw, []byte(secret)) {
		t.Fatalf("colonne secrets = %q, %v", raw, err)
	}

	// Erreur de validation : 422, saisie conservée mais pas le mot de passe.
	bad := url.Values{"mode": {"custom"}, "f_smtp_host": {""}, "f_smtp_from": {"x"}, "x_smtp_password": {secret + "-bis"}}
	rec = c.do("POST", transportPath+"/email", bad)
	mustContain(t, rec, 422, "Serveur SMTP")
	if strings.Contains(rec.Body.String(), "DOCTOLIBRE_") || strings.Contains(rec.Body.String(), secret) {
		t.Error("variable d'environnement ou secret dans la réponse")
	}

	// Message de test : succès affiché (faux sender), vers l'email de l'utilisateur connecté.
	if rec := c.do("POST", transportPath+"/email/test", url.Values{}); rec.Code != 303 {
		t.Fatalf("test : %d", rec.Code)
	}
	mustContain(t, c.do("GET", transportPath, nil), 200, "Message de test envoyé")
	if len(sink.to) != 1 || sink.to[0] != userEmail {
		t.Errorf("destinataires = %v", sink.to)
	}
	// Au-delà de 5 tests par heure : refus affiché.
	for range service.TestSendsPerHour {
		c.do("POST", transportPath+"/email/test", url.Values{})
	}
	mustContain(t, c.do("GET", transportPath, nil), 200, "Trop de messages de test")

	// Effacer le mot de passe : plus « défini ».
	form.Del("x_smtp_password")
	form.Set("c_smtp_password", "1")
	form.Set("f_smtp_user", "")
	if rec := c.do("POST", transportPath+"/email", form); rec.Code != 303 {
		t.Fatalf("effacer : %d", rec.Code)
	}
	rec = c.do("GET", transportPath, nil)
	if strings.Contains(rec.Body.String(), `badge-success badge-soft">défini`) {
		t.Error("secret encore marqué défini après effacement")
	}
}

func TestTransportDisableUpdatesDashboardAndSettings(t *testing.T) {
	t.Parallel()
	c, _, _ := newTransportAdmin(t, domain.RoleAdmin)
	if rec := c.do("POST", transportPath+"/sms", url.Values{"mode": {"disabled"}}); rec.Code != 303 {
		t.Fatalf("POST : %d", rec.Code)
	}
	mustContain(t, c.do("GET", "/admin", nil), 200, "Le SMS est désactivé")
	rec := c.do("GET", "/admin/settings/notifications", nil)
	mustContain(t, rec, 200, "Le SMS est désactivé pour votre cabinet", `value="email"`)
	if strings.Contains(rec.Body.String(), `<option value="sms"`) || strings.Contains(rec.Body.String(), `<option value="both"`) {
		t.Error("canal désactivé encore proposé")
	}
}

func TestTransportPermissions(t *testing.T) {
	t.Parallel()
	// Les fournisseurs engagent le cabinet : ni le secrétariat ni un praticien (T056).
	for _, role := range []string{domain.RoleSecretary, domain.RolePractitioner} {
		c, _, _ := newTransportAdmin(t, role)
		for _, req := range []struct{ method, path string }{
			{"GET", transportPath}, {"POST", transportPath + "/email"}, {"POST", transportPath + "/sms/test"},
		} {
			if rec := c.do(req.method, req.path, url.Values{"mode": {"disabled"}}); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s %s : %d", role, req.method, req.path, rec.Code)
			}
		}
	}
	a, _, _ := newTransportAdmin(t, domain.RoleAdmin)
	if rec := a.do("POST", transportPath+"/fax", url.Values{"mode": {"server"}}); rec.Code != http.StatusNotFound {
		t.Errorf("canal inconnu : %d", rec.Code)
	}
}

func TestPublicBookingClosedWithoutChannel(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	ctx := context.Background()
	c := newClient(t, e.h, "192.0.2.77")
	tid := e.typ.ID.String()

	if _, err := e.db.Exec(ctx, `INSERT INTO practice_notify_transport (practice_id, kind, mode) VALUES ($1, 'sms', 'disabled')`, e.pc.ID); err != nil {
		t.Fatal(err)
	}
	// SMS seul désactivé : le formulaire ne propose que l'email.
	rec := c.do("GET", e.base()+"/book/details?type="+tid+"&start=2026-10-20T07:00:00Z", nil)
	mustContain(t, rec, 200, `name="channel" value="email" checked`)
	if strings.Contains(rec.Body.String(), `name="channel" value="sms"`) {
		t.Error("SMS proposé alors qu'il est désactivé")
	}

	// Plus aucun canal : pages fermées, aucun code.
	if _, err := e.db.Exec(ctx, `INSERT INTO practice_notify_transport (practice_id, kind, mode) VALUES ($1, 'email', 'disabled')`, e.pc.ID); err != nil {
		t.Fatal(err)
	}
	mustContain(t, c.do("GET", "/p/"+e.pc.Slug, nil), 200, "La réservation en ligne n'est pas disponible")
	mustContain(t, c.do("GET", e.base(), nil), 200, "La réservation en ligne n'est pas disponible")
	mustContain(t, c.do("GET", e.base()+"/book?type="+tid, nil), 503, "contacter le cabinet")
	form := detailsForm(tid, "2026-10-20T07:00:00Z")
	before := e.otp.count()
	mustContain(t, c.do("POST", e.base()+"/book/details", form), 503, "contacter le cabinet")
	if e.otp.count() != before {
		t.Error("code envoyé alors que la réservation est fermée")
	}
}

func TestPublicWaitlistClosedWithoutChannel(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	withWaitlist(t, e)
	c := newClient(t, e.h, "192.0.2.78")
	tid := e.typ.ID.String()
	if _, err := e.db.Exec(context.Background(), `INSERT INTO practice_notify_transport (practice_id, kind, mode) VALUES ($1, 'sms', 'disabled'), ($1, 'email', 'disabled')`, e.pc.ID); err != nil {
		t.Fatal(err)
	}
	mustContain(t, c.do("GET", e.base()+"/waitlist?type="+tid, nil), 503, "contacter le cabinet")
	before := e.otp.count()
	mustContain(t, c.do("POST", e.base()+"/waitlist", waitlistForm(tid)), 503, "contacter le cabinet")
	if e.otp.count() != before {
		t.Error("code envoyé alors que la liste d'attente est fermée")
	}
}
