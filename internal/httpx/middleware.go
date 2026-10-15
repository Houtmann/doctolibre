package httpx

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Middleware est un décorateur de handler.
type Middleware func(http.Handler) http.Handler

// Chain applique les middlewares : le premier de la liste est le plus externe.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyLogger
	keyCSRF
	keyClientIP
)

// RequestIDHeader est le header portant l'identifiant de requête.
const RequestIDHeader = "X-Request-ID"

// RequestIDFrom retourne l'identifiant de requête du contexte ("" si absent).
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(keyRequestID).(string)
	return id
}

// LoggerFrom retourne le logger enrichi du request id, ou slog.Default().
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(keyLogger).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

func validRequestID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// RequestID reprend le X-Request-ID entrant s'il est sain, sinon en génère un ;
// l'identifiant est renvoyé en header et placé dans le contexte et le logger.
func RequestID(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if !validRequestID(id) {
				b := make([]byte, 8)
				_, _ = rand.Read(b)
				id = hex.EncodeToString(b)
			}
			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), keyRequestID, id)
			ctx = context.WithValue(ctx, keyLogger, logger.With(slog.String("request_id", id)))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap permet à http.ResponseController d'atteindre le writer d'origine.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Logger journalise chaque requête : méthode, chemin (sans query string), statut,
// durée. Jamais de query string, de body ni de header.
func Logger() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			LoggerFrom(r.Context()).Info("requête",
				slog.String("method", r.Method),
				slog.String("path", loggedPath(r.URL.Path)),
				slog.Int("status", rec.status),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// secretPrefixes : chemins dont le segment suivant est un jeton secret (flux ICS,
// lien patient) ; il ne doit jamais apparaître dans les logs.
var secretPrefixes = []string{"/ics/", "/r/"}

func loggedPath(p string) string {
	for _, pre := range secretPrefixes {
		if strings.HasPrefix(p, pre) {
			return pre + "…"
		}
	}
	return p
}

// Recover transforme une panic en 500 (texte brut) et journalise la stack.
func Recover() Middleware { return RecoverWith(Error) }

// RecoverWith est Recover avec un rendu d'erreur personnalisé (page HTML stylée).
func RecoverWith(onPanic func(w http.ResponseWriter, r *http.Request, status int, msg string)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler { //nolint:errorlint // sentinelle de panic
					panic(rec)
				}
				LoggerFrom(r.Context()).Error("panic dans un handler",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				onPanic(w, r, http.StatusInternalServerError, "erreur interne")
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecureHeaders ajoute les headers de sécurité ; HSTS seulement si prod.
func SecureHeaders(prod bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			h.Set("X-Frame-Options", "DENY")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			if prod {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Noms et limites CSRF.
const (
	CSRFCookie     = "csrf_token"
	CSRFHeader     = "X-CSRF-Token"
	CSRFFormField  = "csrf_token"
	maxCSRFFormLen = 1 << 20
)

// CSRFToken retourne le jeton CSRF de la requête, à inclure dans les formulaires
// (champ caché `csrf_token`) ou dans hx-headers (`X-CSRF-Token`).
func CSRFToken(ctx context.Context) string {
	t, _ := ctx.Value(keyCSRF).(string)
	return t
}

// CSRF implémente le double-submit cookie : le cookie (HttpOnly, SameSite=Lax)
// est posé si absent ; toute méthode autre que GET/HEAD/OPTIONS doit renvoyer le
// même jeton via le header X-CSRF-Token (htmx) ou le champ de formulaire csrf_token.
func CSRF(secure bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var token string
			if c, err := r.Cookie(CSRFCookie); err == nil && len(c.Value) >= 32 {
				token = c.Value
			} else {
				b := make([]byte, 32)
				_, _ = rand.Read(b)
				token = base64.RawURLEncoding.EncodeToString(b)
				http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure dépend de l'env (false en dev http)
					Name: CSRFCookie, Value: token, Path: "/",
					HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
				})
			}
			if !safeMethod(r.Method) {
				if _, err := r.Cookie(CSRFCookie); err != nil {
					Error(w, r, http.StatusForbidden, "jeton CSRF invalide")
					return
				}
				got := r.Header.Get(CSRFHeader)
				if got == "" {
					r.Body = http.MaxBytesReader(w, r.Body, maxCSRFFormLen)
					got = r.PostFormValue(CSRFFormField)
				}
				if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
					Error(w, r, http.StatusForbidden, "jeton CSRF invalide")
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyCSRF, token)))
		})
	}
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// ClientIP retourne l'IP du client (sans port). Si le middleware TrustedProxies
// est actif, c'est l'IP résolue via X-Forwarded-For ; sinon RemoteAddr.
func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(keyClientIP).(string); ok {
		return ip
	}
	return remoteIP(r)
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// TrustedProxies résout l'IP du client derrière un reverse proxy : X-Forwarded-For
// n'est lu que si RemoteAddr appartient à `trusted`. La liste est parcourue de
// droite à gauche et la première IP non fiable est retenue (si toutes sont
// fiables, la plus à gauche). En-tête invalide -> RemoteAddr. X-Real-IP est ignoré.
// Sans proxy configuré, le middleware ne change rien.
func TrustedProxies(trusted []netip.Prefix) Middleware {
	return func(next http.Handler) http.Handler {
		if len(trusted) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := resolveClientIP(r, trusted)
			ctx := context.WithValue(r.Context(), keyClientIP, ip)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	a = a.Unmap()
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func resolveClientIP(r *http.Request, trusted []netip.Prefix) string {
	remote := remoteIP(r)
	ra, err := netip.ParseAddr(remote)
	if err != nil || !isTrusted(ra, trusted) {
		return remote
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return remote
	}
	var hops []netip.Addr
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			a, err := netip.ParseAddr(strings.TrimSpace(part))
			if err != nil {
				return remote
			}
			hops = append(hops, a.Unmap())
		}
	}
	for i := len(hops) - 1; i >= 0; i-- {
		if !isTrusted(hops[i], trusted) {
			return hops[i].String()
		}
	}
	return hops[0].String()
}

type visitor struct {
	lim  *rate.Limiter
	seen time.Time
}

const (
	sweepEvery  = time.Minute
	visitorIdle = 10 * time.Minute
)

// RateLimit limite par clé (ex. ClientIP) à `r` requêtes/s avec rafale `burst`,
// en mémoire ; les clés inactives sont évincées périodiquement. Dépassement → 429.
func RateLimit(key func(*http.Request) string, r rate.Limit, burst int) Middleware {
	return rateLimit(key, r, burst, time.Now)
}

func rateLimit(key func(*http.Request) string, r rate.Limit, burst int, now func() time.Time) Middleware {
	var (
		mu        sync.Mutex
		visitors  = map[string]*visitor{}
		lastSweep = now()
	)
	allow := func(k string) bool {
		mu.Lock()
		defer mu.Unlock()
		t := now()
		if t.Sub(lastSweep) > sweepEvery {
			for kk, v := range visitors {
				if t.Sub(v.seen) > visitorIdle {
					delete(visitors, kk)
				}
			}
			lastSweep = t
		}
		v, ok := visitors[k]
		if !ok {
			v = &visitor{lim: rate.NewLimiter(r, burst)}
			visitors[k] = v
		}
		v.seen = t
		return v.lim.AllowN(t, 1)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !allow(key(req)) {
				w.Header().Set("Retry-After", "1")
				Error(w, req, http.StatusTooManyRequests, "trop de requêtes")
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}
