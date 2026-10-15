package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/testutil"
)

const testPassword = "un-mot-de-passe-solide" //nolint:gosec // mot de passe de test

var t0 = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func setup(t *testing.T) (*auth.Service, *testutil.FixedClock, *pgxpool.Pool) {
	t.Helper()
	db := testutil.DB(t)
	clock := testutil.NewFixedClock(t0)
	svc := &auth.Service{Pool: db, Clock: clock}
	if _, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{
		Email: "Kine@Example.test", Role: domain.RoleAdmin, PracticeName: "Cabinet test", Password: testPassword,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return svc, clock, db
}

func count(t *testing.T, db *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLoginAndAuthenticate(t *testing.T) {
	t.Parallel()
	svc, clock, db := setup(t)
	ctx := context.Background()

	sess, err := svc.Login(ctx, auth.LoginParams{Email: " kine@example.TEST ", Password: testPassword, IP: "203.0.113.7"})
	if err != nil {
		t.Fatal(err)
	}
	if sess.Token == "" || !sess.ExpiresAt.Equal(t0.Add(auth.DefaultTTL)) {
		t.Fatalf("session inattendue: %+v", sess)
	}
	// Le token n'est jamais stocké en clair : seul sha256(token) l'est.
	if n := count(t, db, `SELECT count(*) FROM session WHERE id = $1`, auth.SessionID(sess.Token)); n != 1 {
		t.Errorf("session hashée absente")
	}
	if n := count(t, db, `SELECT count(*) FROM session WHERE id = $1`, sess.Token); n != 0 {
		t.Errorf("token en clair en base")
	}
	if n := count(t, db, `SELECT count(*) FROM audit_log WHERE action = 'login_success' AND user_id = $1`, sess.User.ID); n != 1 {
		t.Errorf("audit login_success: %d", n)
	}

	got, err := svc.Authenticate(ctx, sess.Token)
	if err != nil || got.User.ID != sess.User.ID || got.User.Role != domain.RoleAdmin {
		t.Fatalf("Authenticate: %+v, %v", got, err)
	}
	if got.Refreshed {
		t.Error("pas de prolongation attendue immédiatement")
	}

	// Expiration glissante : 11 h plus tard la session est prolongée de 12 h.
	clock.Advance(11 * time.Hour)
	got, err = svc.Authenticate(ctx, sess.Token)
	if err != nil || !got.Refreshed || !got.ExpiresAt.Equal(clock.Now().Add(auth.DefaultTTL)) {
		t.Fatalf("prolongation: %+v, %v", got, err)
	}
	// 11 h + 11 h : toujours valide grâce à la prolongation ; puis 13 h d'inactivité : expirée.
	clock.Advance(11 * time.Hour)
	if _, err := svc.Authenticate(ctx, sess.Token); err != nil {
		t.Fatalf("session prolongée refusée: %v", err)
	}
	clock.Advance(13 * time.Hour)
	if _, err := svc.Authenticate(ctx, sess.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("session expirée acceptée: %v", err)
	}
}

func TestSessionAbsoluteLimit(t *testing.T) {
	t.Parallel()
	svc, clock, _ := setup(t)
	ctx := context.Background()
	sess, err := svc.Login(ctx, auth.LoginParams{Email: "kine@example.test", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	// Activité régulière toutes les 10 h : la session reste valide jusqu'à 7 jours, pas au-delà.
	for i := 0; i < 16; i++ { // 160 h < 168 h
		clock.Advance(10 * time.Hour)
		if _, err := svc.Authenticate(ctx, sess.Token); err != nil {
			t.Fatalf("itération %d: %v", i, err)
		}
	}
	clock.Advance(10 * time.Hour) // 170 h > 168 h
	if _, err := svc.Authenticate(ctx, sess.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("limite absolue non appliquée: %v", err)
	}
}

func TestLoginFailures(t *testing.T) {
	t.Parallel()
	svc, _, db := setup(t)
	ctx := context.Background()
	for _, p := range []auth.LoginParams{
		{Email: "kine@example.test", Password: "mauvais"},
		{Email: "inconnu@example.test", Password: testPassword},
		{Email: "", Password: ""},
	} {
		sess, err := svc.Login(ctx, p)
		if !errors.Is(err, auth.ErrInvalidCredentials) || sess.Token != "" {
			t.Errorf("Login(%q) = %+v, %v", p.Email, sess, err)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM audit_log WHERE action = 'login_failure'`); n != 3 {
		t.Errorf("audit login_failure: %d", n)
	}
	// Ni email ni mot de passe dans l'audit.
	if n := count(t, db, `SELECT count(*) FROM audit_log WHERE meta::text ILIKE '%example.test%' OR meta::text ILIKE '%mauvais%'`); n != 0 {
		t.Errorf("données personnelles dans l'audit")
	}
	if n := count(t, db, `SELECT count(*) FROM session`); n != 0 {
		t.Errorf("session créée malgré l'échec")
	}
}

func TestLoginRotatesSessionAndLogoutRevokes(t *testing.T) {
	t.Parallel()
	svc, _, db := setup(t)
	ctx := context.Background()
	first, err := svc.Login(ctx, auth.LoginParams{Email: "kine@example.test", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Login(ctx, auth.LoginParams{Email: "kine@example.test", Password: testPassword, OldToken: first.Token})
	if err != nil {
		t.Fatal(err)
	}
	if second.Token == first.Token {
		t.Fatal("token non renouvelé au login")
	}
	if _, err := svc.Authenticate(ctx, first.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("ancienne session encore valide: %v", err)
	}
	if err := svc.Logout(ctx, second.Token, second.User, "203.0.113.7"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, second.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("session valide après logout: %v", err)
	}
	if n := count(t, db, `SELECT count(*) FROM session`); n != 0 {
		t.Errorf("sessions restantes: %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM audit_log WHERE action = 'logout'`); n != 1 {
		t.Errorf("audit logout: %d", n)
	}
}

func TestAuthenticateGarbage(t *testing.T) {
	t.Parallel()
	svc, _, _ := setup(t)
	for _, tok := range []string{"", "nope", strings.Repeat("a", 500)} {
		if _, err := svc.Authenticate(context.Background(), tok); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Errorf("token %q: %v", tok, err)
		}
	}
}

func TestRequireUser(t *testing.T) {
	t.Parallel()
	svc, _, _ := setup(t)
	sess, err := svc.Login(context.Background(), auth.LoginParams{Email: "kine@example.test", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, found := auth.UserFrom(r.Context())
		if !found || u.ID != sess.User.ID {
			t.Errorf("utilisateur absent du contexte")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	do := func(h http.Handler, method, target string, withCookie bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		if withCookie {
			req.AddCookie(&http.Cookie{Name: svc.CookieName(), Value: sess.Token}) //nolint:gosec // cookie de requête simulé
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	anyRole := svc.RequireUser()(ok)
	if rec := do(anyRole, "GET", "/admin/x?a=1&b=2", false); rec.Code != 303 || rec.Header().Get("Location") != "/admin/login?next=%2Fadmin%2Fx%3Fa%3D1%26b%3D2" {
		t.Errorf("sans session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := do(anyRole, "POST", "/admin/x", false); rec.Code != 303 || rec.Header().Get("Location") != "/admin/login" {
		t.Errorf("POST sans session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := do(anyRole, "GET", "/admin", true); rec.Code != 204 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("avec session: %d", rec.Code)
	}
	secretaryOnly := svc.RequireUser(domain.RoleSecretary)(ok)
	if rec := do(secretaryOnly, "GET", "/admin", true); rec.Code != 403 {
		t.Errorf("rôle insuffisant: %d", rec.Code)
	}
	adminOnly := svc.RequireUser(domain.RoleAdmin, domain.RoleSecretary)(ok)
	if rec := do(adminOnly, "GET", "/admin", true); rec.Code != 204 {
		t.Errorf("rôle suffisant: %d", rec.Code)
	}
}

func TestCreateUserValidation(t *testing.T) {
	t.Parallel()
	_, _, db := setup(t)
	ctx := context.Background()
	base := auth.CreateUserParams{Email: "a@example.test", Role: domain.RoleSecretary, PracticeName: "Cabinet test", Password: testPassword}
	tests := []struct {
		name string
		mut  func(*auth.CreateUserParams)
	}{
		{"email invalide", func(p *auth.CreateUserParams) { p.Email = "pas-un-email" }},
		{"email avec nom", func(p *auth.CreateUserParams) { p.Email = "Bob <b@example.test>" }},
		{"rôle inconnu", func(p *auth.CreateUserParams) { p.Role = "root" }},
		{"cabinet vide", func(p *auth.CreateUserParams) { p.PracticeName = " " }},
		{"mot de passe court", func(p *auth.CreateUserParams) { p.Password = "court" }},
		{"email déjà pris", func(p *auth.CreateUserParams) { p.Email = "KINE@example.test" }},
	}
	for _, tt := range tests {
		p := base
		tt.mut(&p)
		var ve domain.ErrValidation
		if _, err := auth.CreateUser(ctx, db, p); !errors.As(err, &ve) {
			t.Errorf("%s: erreur de validation attendue, got %v", tt.name, err)
		}
	}
	// Le cabinet existant (même nom) est réutilisé.
	if _, err := auth.CreateUser(ctx, db, base); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM practice`); n != 1 {
		t.Errorf("cabinets: %d, attendu 1", n)
	}
	// Mot de passe stocké haché argon2id.
	if n := count(t, db, `SELECT count(*) FROM app_user WHERE password_hash LIKE '$argon2id$%'`); n != 2 {
		t.Errorf("hashes argon2id: %d", n)
	}
}
