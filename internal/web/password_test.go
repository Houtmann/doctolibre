package web_test

import (
	"context"
	"encoding/json"
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
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

const newPw = "un-autre-mot-de-passe-solide" //nolint:gosec // mot de passe de test

func newResetEnv(t *testing.T) (http.Handler, *testutil.FixedClock, *pgxpool.Pool) {
	t.Helper()
	db := testutil.DB(t)
	if _, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{
		Email: userEmail, Role: domain.RoleAdmin, PracticeName: "Cabinet test", Password: pw,
	}); err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev, BaseURL: "https://rdv.example.test"},
		Logger: logger,
		DB:     db,
		Auth:   &auth.Service{Pool: db, Clock: clock, Logger: logger},
	})
	return h, clock, db
}

type queuedMail struct {
	Recipient string
	ResetURL  string
}

func queuedMails(t *testing.T, db *pgxpool.Pool) []queuedMail {
	t.Helper()
	rows, err := db.Query(context.Background(),
		`SELECT recipient, payload FROM outbox_message WHERE kind = 'email' AND template = 'password_reset' ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []queuedMail
	for rows.Next() {
		var rcpt string
		var payload []byte
		if err := rows.Scan(&rcpt, &payload); err != nil {
			t.Fatal(err)
		}
		var p struct {
			ResetURL string `json:"reset_url"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			t.Fatal(err)
		}
		out = append(out, queuedMail{Recipient: rcpt, ResetURL: p.ResetURL})
	}
	return out
}

func tokenOf(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil || u.Path != "/admin/password/reset" || u.Host != "rdv.example.test" {
		t.Fatalf("lien inattendu %q (%v)", link, err)
	}
	return u.Query().Get("token")
}

func TestPasswordResetEndToEnd(t *testing.T) {
	t.Parallel()
	h, _, db := newResetEnv(t)
	c := newClient(t, h, "192.0.2.10")

	// Une session existante doit être révoquée par le changement de mot de passe.
	other := newClient(t, h, "192.0.2.11")
	if rec := other.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login initial: %d", rec.Code)
	}
	if rec := other.do("GET", "/admin", nil); rec.Code != 200 {
		t.Fatalf("session initiale: %d", rec.Code)
	}

	if rec := c.do("GET", "/admin/password/forgot", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="email"`) {
		t.Fatalf("formulaire: %d", rec.Code)
	}
	if rec := c.do("GET", "/admin/login", nil); !strings.Contains(rec.Body.String(), "/admin/password/forgot") {
		t.Error("lien « mot de passe oublié » absent de la page de connexion")
	}

	rec := c.do("POST", "/admin/password/forgot", url.Values{"email": {"  KINE@example.test "}})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("demande: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	mails := queuedMails(t, db)
	if len(mails) != 1 || mails[0].Recipient != userEmail {
		t.Fatalf("outbox: %+v", mails)
	}
	token := tokenOf(t, mails[0].ResetURL)
	if len(token) < 40 {
		t.Fatalf("jeton trop court: %d", len(token))
	}
	// Le jeton n'est stocké que haché.
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM password_reset WHERE token_hash = $1`, token).Scan(&n); err != nil || n != 0 {
		t.Errorf("jeton stocké en clair (n=%d, err=%v)", n, err)
	}
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM password_reset WHERE token_hash = $1`, auth.SessionID(token)).Scan(&n); err != nil || n != 1 {
		t.Errorf("jeton haché absent (n=%d, err=%v)", n, err)
	}

	rec = c.do("GET", "/admin/password/reset?token="+url.QueryEscape(token), nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="password2"`) || !strings.Contains(rec.Body.String(), token) {
		t.Fatalf("formulaire de reset: %d", rec.Code)
	}

	// Mot de passe trop court / différent : jeton conservé.
	for _, f := range []url.Values{
		{"token": {token}, "password": {"court"}, "password2": {"court"}},
		{"token": {token}, "password": {newPw}, "password2": {newPw + "x"}},
	} {
		if rec := c.do("POST", "/admin/password/reset", f); rec.Code != 422 || !strings.Contains(rec.Body.String(), "form-errors") {
			t.Fatalf("mot de passe invalide: %d", rec.Code)
		}
	}

	rec = c.do("POST", "/admin/password/reset", url.Values{"token": {token}, "password": {newPw}, "password2": {newPw}})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("reset: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// L'ancienne session est révoquée, l'ancien mot de passe ne marche plus, le nouveau oui.
	if rec := other.do("GET", "/admin", nil); rec.Code != 303 {
		t.Errorf("ancienne session encore valide: %d", rec.Code)
	}
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 401 {
		t.Errorf("ancien mot de passe accepté: %d", rec.Code)
	}
	if rec := c.login(userEmail, newPw, "/admin"); rec.Code != 303 || rec.Header().Get("Location") != "/admin" {
		t.Errorf("nouveau mot de passe refusé: %d", rec.Code)
	}

	// Usage unique.
	if rec := c.do("GET", "/admin/password/reset?token="+url.QueryEscape(token), nil); rec.Code != 400 {
		t.Errorf("jeton réutilisable (GET): %d", rec.Code)
	}
	rec = c.do("POST", "/admin/password/reset", url.Values{"token": {token}, "password": {pw}, "password2": {pw}})
	if rec.Code != 400 {
		t.Errorf("jeton réutilisable (POST): %d", rec.Code)
	}
	if rec := c.login(userEmail, newPw, "/admin"); rec.Code != 303 {
		t.Errorf("mot de passe changé par un jeton consommé: %d", rec.Code)
	}
}

func TestPasswordResetUnknownEmailIndistinguishable(t *testing.T) {
	t.Parallel()
	h, _, db := newResetEnv(t)
	post := func(email string) (int, string, string) {
		c := newClient(t, h, "192.0.2.20")
		rec := c.do("POST", "/admin/password/forgot", url.Values{"email": {email}})
		follow := c.do("GET", "/admin/login", nil)
		return rec.Code, rec.Header().Get("Location"), follow.Body.String()
	}
	codeK, locK, pageK := post(userEmail)
	codeU, locU, pageU := post("inconnu@example.test")
	codeB, locB, _ := post("pas-un-email")
	if codeK != codeU || locK != locU || codeK != codeB || locK != locB {
		t.Fatalf("réponses distinguables: (%d %q) (%d %q) (%d %q)", codeK, locK, codeU, locU, codeB, locB)
	}
	if !strings.Contains(pageK, "vient d&#39;être envoyé") || !strings.Contains(pageU, "vient d&#39;être envoyé") {
		t.Error("message de confirmation absent")
	}
	if mails := queuedMails(t, db); len(mails) != 1 || mails[0].Recipient != userEmail {
		t.Errorf("outbox: %+v", mails)
	}
}

func TestPasswordResetExpiry(t *testing.T) {
	t.Parallel()
	h, clock, db := newResetEnv(t)
	c := newClient(t, h, "192.0.2.30")
	c.do("POST", "/admin/password/forgot", url.Values{"email": {userEmail}})
	token := tokenOf(t, queuedMails(t, db)[0].ResetURL)

	clock.Advance(auth.ResetTTL - time.Second)
	if rec := c.do("GET", "/admin/password/reset?token="+url.QueryEscape(token), nil); rec.Code != 200 {
		t.Fatalf("jeton valable 59 min 59 s: %d", rec.Code)
	}
	clock.Advance(time.Second)
	if rec := c.do("GET", "/admin/password/reset?token="+url.QueryEscape(token), nil); rec.Code != 400 {
		t.Errorf("jeton expiré accepté (GET): %d", rec.Code)
	}
	rec := c.do("POST", "/admin/password/reset", url.Values{"token": {token}, "password": {newPw}, "password2": {newPw}})
	if rec.Code != 400 {
		t.Errorf("jeton expiré accepté (POST): %d", rec.Code)
	}
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Errorf("mot de passe modifié par un jeton expiré: %d", rec.Code)
	}
}

func TestPasswordResetNewRequestInvalidatesPrevious(t *testing.T) {
	t.Parallel()
	h, _, db := newResetEnv(t)
	c := newClient(t, h, "192.0.2.40")
	c.do("POST", "/admin/password/forgot", url.Values{"email": {userEmail}})
	c.do("POST", "/admin/password/forgot", url.Values{"email": {userEmail}})
	mails := queuedMails(t, db)
	if len(mails) != 2 {
		t.Fatalf("outbox: %d", len(mails))
	}
	first, second := tokenOf(t, mails[0].ResetURL), tokenOf(t, mails[1].ResetURL)
	if rec := c.do("GET", "/admin/password/reset?token="+url.QueryEscape(first), nil); rec.Code != 400 {
		t.Errorf("ancien jeton encore valide: %d", rec.Code)
	}
	if rec := c.do("GET", "/admin/password/reset?token="+url.QueryEscape(second), nil); rec.Code != 200 {
		t.Errorf("dernier jeton invalide: %d", rec.Code)
	}
	if rec := c.do("GET", "/admin/password/reset", nil); rec.Code != 400 {
		t.Errorf("sans jeton: %d", rec.Code)
	}
}

func TestPasswordForgotRateLimited(t *testing.T) {
	t.Parallel()
	h, _, db := newResetEnv(t)
	c := newClient(t, h, "192.0.2.50")
	var last int
	for i := 0; i < 7; i++ {
		last = c.do("POST", "/admin/password/forgot", url.Values{"email": {userEmail}}).Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("pas de limitation de débit: %d", last)
	}
	if mails := queuedMails(t, db); len(mails) > 5 {
		t.Errorf("%d emails en file malgré la limite", len(mails))
	}
}
