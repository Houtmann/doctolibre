package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Durées par défaut des sessions.
const (
	// DefaultTTL : une session expire après 12 h d'inactivité (expiration glissante).
	DefaultTTL = 12 * time.Hour
	// DefaultMaxAge : durée de vie absolue, quelle que soit l'activité.
	DefaultMaxAge = 7 * 24 * time.Hour

	// refreshEvery limite les écritures : l'expiration n'est repoussée que si au
	// moins cette durée s'est écoulée depuis la dernière extension.
	refreshEvery = time.Minute

	tokenBytes   = 32
	maxUserAgent = 256

	// CookieName / DevCookieName : le préfixe __Host- exige Secure, donc il n'est
	// utilisable qu'en prod (HTTPS) ; en dev (HTTP) le navigateur le rejetterait.
	CookieName    = "__Host-session"
	DevCookieName = "session"
)

// Actions d'audit.
const (
	ActionLoginSuccess = "login_success"
	ActionLoginFailure = "login_failure"
	ActionLogout       = "logout"
)

// Erreurs d'authentification.
var (
	// ErrInvalidCredentials : utilisateur inconnu ou mot de passe incorrect (indistinguables).
	ErrInvalidCredentials = errors.New("identifiants invalides")
	// ErrUnauthenticated : session absente, expirée ou révoquée.
	ErrUnauthenticated = errors.New("non authentifié")
)

// User est l'utilisateur authentifié placé dans le contexte de la requête.
type User struct {
	ID             uuid.UUID
	PracticeID     uuid.UUID
	PractitionerID uuid.NullUUID
	Email          string // ne jamais journaliser
	Role           string
	// Delegated : praticiens dont l'agenda est délégué à l'utilisateur au jour
	// courant du cabinet (T059). Rechargé à chaque requête par Authenticate,
	// jamais stocké dans la session.
	Delegated []uuid.UUID
}

type ctxKey struct{}

// WithUser place u dans le contexte (utilisé par le middleware et les tests).
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// UserFrom retourne l'utilisateur authentifié du contexte.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

// Service gère connexion, sessions et déconnexion.
type Service struct {
	Pool   *pgxpool.Pool
	Clock  domain.Clock
	Logger *slog.Logger  // optionnel
	Secure bool          // cookie Secure + préfixe __Host- (prod)
	TTL    time.Duration // 0 = DefaultTTL
	MaxAge time.Duration // 0 = DefaultMaxAge
}

func (s *Service) ttl() time.Duration {
	if s.TTL > 0 {
		return s.TTL
	}
	return DefaultTTL
}

func (s *Service) maxAge() time.Duration {
	if s.MaxAge > 0 {
		return s.MaxAge
	}
	return DefaultMaxAge
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// CookieName retourne le nom du cookie de session pour cet environnement.
func (s *Service) CookieName() string {
	if s.Secure {
		return CookieName
	}
	return DevCookieName
}

// SessionID retourne l'identifiant en base d'un token : sha256 hexadécimal.
func SessionID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// LoginParams regroupe les entrées d'une connexion.
type LoginParams struct {
	Email     string
	Password  string
	OldToken  string // session présentée avec la requête, révoquée (rotation)
	IP        string
	UserAgent string
}

// Session est le résultat d'une connexion réussie ou d'une authentification.
type Session struct {
	Token     string // vide pour Authenticate
	User      User
	ExpiresAt time.Time
	Refreshed bool // expiration repoussée : le cookie doit être ré-émis
}

func userFromRow(r sqlcgen.AppUser) User {
	return User{
		ID: r.ID, PracticeID: r.PracticeID,
		PractitionerID: uuid.NullUUID{UUID: r.PractitionerID.Bytes, Valid: r.PractitionerID.Valid},
		Email:          r.Email, Role: r.Role,
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return strings.ToValidUTF8(s[:n], "")
	}
	return s
}

func auditMeta(ip string) []byte {
	b, _ := json.Marshal(map[string]string{"ip": ip})
	return b
}

func (s *Service) audit(ctx context.Context, q *sqlcgen.Queries, userID uuid.NullUUID, action, ip string) error {
	actor := "anonymous"
	var entityID pgtype.UUID
	if userID.Valid {
		actor = "user:" + userID.UUID.String()
		entityID = pgtype.UUID{Bytes: userID.UUID, Valid: true}
	}
	return q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		UserID:   pgtype.UUID{Bytes: userID.UUID, Valid: userID.Valid},
		Actor:    actor,
		Action:   action,
		Entity:   "user",
		EntityID: entityID,
		Meta:     auditMeta(ip),
	})
}

// Login vérifie les identifiants et ouvre une session avec un token neuf (la
// session précédente, s'il y en a une, est révoquée : pas de fixation de session).
// Utilisateur inconnu et mauvais mot de passe renvoient la même erreur, après un
// travail de hachage équivalent.
func (s *Service) Login(ctx context.Context, p LoginParams) (Session, error) {
	now := s.Clock.Now()
	email := strings.ToLower(strings.TrimSpace(p.Email))
	q := sqlcgen.New(s.Pool)

	row, err := q.GetUserByEmail(ctx, email)
	var found bool
	switch {
	case err == nil:
		found = true
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return Session{}, fmt.Errorf("lecture utilisateur: %w", err)
	}

	ok := false
	if found {
		var verr error
		ok, verr = Verify(p.Password, row.PasswordHash)
		if verr != nil {
			s.logger().Error("hash de mot de passe inexploitable", slog.String("user_id", row.ID.String()))
		}
		// Compte désactivé : mêmes réponse et travail que pour un mauvais mot de passe.
		ok = ok && row.Active
	} else {
		dummyVerify(p.Password)
	}
	if !ok {
		var uid uuid.NullUUID
		if found {
			uid = uuid.NullUUID{UUID: row.ID, Valid: true}
		}
		if aerr := s.audit(ctx, q, uid, ActionLoginFailure, p.IP); aerr != nil {
			s.logger().Error("audit login_failure en échec", slog.String("err", aerr.Error()))
		}
		return Session{}, ErrInvalidCredentials
	}

	token, err := newToken()
	if err != nil {
		return Session{}, err
	}
	exp := now.Add(s.ttl())
	err = store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		if p.OldToken != "" {
			if err := q.DeleteSession(ctx, SessionID(p.OldToken)); err != nil {
				return fmt.Errorf("révocation ancienne session: %w", err)
			}
		}
		if _, err := q.CreateSession(ctx, sqlcgen.CreateSessionParams{
			ID: SessionID(token), UserID: row.ID, CreatedAt: now, ExpiresAt: exp,
			UserAgent: truncate(p.UserAgent, maxUserAgent), Ip: truncate(p.IP, 64),
		}); err != nil {
			return fmt.Errorf("création session: %w", err)
		}
		if err := q.TouchUserLogin(ctx, sqlcgen.TouchUserLoginParams{ID: row.ID, At: pgtype.Timestamptz{Time: now, Valid: true}}); err != nil {
			return fmt.Errorf("last_login_at: %w", err)
		}
		return s.audit(ctx, q, uuid.NullUUID{UUID: row.ID, Valid: true}, ActionLoginSuccess, p.IP)
	})
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, User: userFromRow(row), ExpiresAt: exp}, nil
}

// Authenticate valide un token de session et repousse son expiration (glissante,
// plafonnée par MaxAge depuis la création). ErrUnauthenticated si la session est
// inconnue, expirée ou révoquée.
func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" || len(token) > 128 {
		return Session{}, ErrUnauthenticated
	}
	now := s.Clock.Now()
	q := sqlcgen.New(s.Pool)
	id := SessionID(token)
	sess, err := q.GetSession(ctx, sqlcgen.GetSessionParams{ID: id, Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrUnauthenticated
	}
	if err != nil {
		return Session{}, fmt.Errorf("lecture session: %w", err)
	}
	hardLimit := sess.CreatedAt.Add(s.maxAge())
	if !now.Before(hardLimit) {
		_ = q.DeleteSession(ctx, id)
		return Session{}, ErrUnauthenticated
	}
	row, err := q.GetUser(ctx, sess.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrUnauthenticated
	}
	if err != nil {
		return Session{}, fmt.Errorf("lecture utilisateur: %w", err)
	}
	if !row.Active {
		_ = q.DeleteSession(ctx, id)
		return Session{}, ErrUnauthenticated
	}
	exp := sess.ExpiresAt
	refreshed := false
	if want := minTime(now.Add(s.ttl()), hardLimit); want.Sub(exp) >= refreshEvery {
		if err := q.ExtendSession(ctx, sqlcgen.ExtendSessionParams{ID: id, ExpiresAt: want}); err != nil {
			return Session{}, fmt.Errorf("extension session: %w", err)
		}
		exp, refreshed = want, true
	}
	user := userFromRow(row)
	if user.Delegated, err = q.ListActiveDelegatedPractitioners(ctx, sqlcgen.ListActiveDelegatedPractitionersParams{
		UserID: row.ID, PracticeID: row.PracticeID, Now: now,
	}); err != nil {
		return Session{}, fmt.Errorf("lecture des délégations: %w", err)
	}
	return Session{User: user, ExpiresAt: exp, Refreshed: refreshed}, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Logout révoque la session en base et journalise l'événement.
func (s *Service) Logout(ctx context.Context, token string, u User, ip string) error {
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		if err := q.DeleteSession(ctx, SessionID(token)); err != nil {
			return fmt.Errorf("suppression session: %w", err)
		}
		return s.audit(ctx, q, uuid.NullUUID{UUID: u.ID, Valid: true}, ActionLogout, ip)
	})
}

// --- Cookie ---

// TokenFrom lit le token de session de la requête ("" si absent).
func (s *Service) TokenFrom(r *http.Request) string {
	c, err := r.Cookie(s.CookieName())
	if err != nil {
		return ""
	}
	return c.Value
}

// SetCookie pose le cookie de session (HttpOnly, SameSite=Lax, Secure en prod).
func (s *Service) SetCookie(w http.ResponseWriter, token string, expires time.Time) {
	maxAge := int(expires.Sub(s.Clock.Now()).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure dépend de l'env (false en dev http)
		Name: s.CookieName(), Value: token, Path: "/",
		Expires: expires, MaxAge: maxAge,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie efface le cookie de session.
func (s *Service) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure dépend de l'env (false en dev http)
		Name: s.CookieName(), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

// --- Middleware ---

// LoginPath est la page de connexion.
const LoginPath = "/admin/login"

// RequireUser exige une session valide et, si roles est non vide, l'un de ces
// rôles. Sans session : redirection vers /admin/login?next=… (GET seulement).
// Charge l'utilisateur dans le contexte (auth.UserFrom).
func (s *Service) RequireUser(roles ...string) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			token := s.TokenFrom(r)
			sess, err := s.Authenticate(r.Context(), token)
			switch {
			case errors.Is(err, ErrUnauthenticated):
				if token != "" {
					s.ClearCookie(w)
				}
				dest := LoginPath
				if r.Method == http.MethodGet {
					dest += "?next=" + queryEscape(r.URL.RequestURI())
				}
				httpx.Redirect(w, r, dest)
				return
			case err != nil:
				httpx.LoggerFrom(r.Context()).Error("authentification en échec", slog.String("err", err.Error()))
				httpx.Error(w, r, http.StatusInternalServerError, "erreur interne")
				return
			}
			if len(roles) > 0 && !hasRole(roles, sess.User.Role) {
				httpx.LoggerFrom(r.Context()).Warn("rôle insuffisant", slog.String("user_id", sess.User.ID.String()))
				httpx.Error(w, r, http.StatusForbidden, "accès refusé")
				return
			}
			if sess.Refreshed {
				s.SetCookie(w, token, sess.ExpiresAt)
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), sess.User)))
		})
	}
}

func hasRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
