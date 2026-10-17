package httpx

import (
	"fmt"
	"html"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// maxVisitors borne le nombre de clés suivies par limiteur.
	maxVisitors = 50_000
	sweepEvery  = time.Minute
	// forcedSweepEvery espace les balayages déclenchés par une map pleine (coût O(n)).
	forcedSweepEvery  = time.Second
	visitorIdle       = 10 * time.Minute
	overflowWarnEvery = time.Minute
)

// IPKey est la clé de limitation par client : l'IP pour IPv4 (ou IPv4 mappée),
// le préfixe /64 pour IPv6 (une adresse tournante dans son /64 ne fabrique pas de clés infinies).
func IPKey(r *http.Request) string {
	raw := ClientIP(r)
	a, err := netip.ParseAddr(raw)
	if err != nil {
		return raw
	}
	a = a.Unmap()
	if a.Is6() {
		if p, err := a.Prefix(64); err == nil {
			return p.String()
		}
	}
	return a.String()
}

type visitor struct {
	lim  *rate.Limiter
	seen time.Time
}

// Limiter est un limiteur à seaux de jetons par clé, en mémoire et à taille bornée.
// Un même *Limiter appliqué à plusieurs routes partage ses seaux : c'est ainsi
// qu'on fixe un quota par parcours plutôt que par route.
type Limiter struct {
	name  string
	key   func(*http.Request) string
	r     rate.Limit
	burst int

	// Réglages (injectables en test).
	now         func() time.Time
	maxVisitors int
	onLimit     func(name string)
	reject      func(w http.ResponseWriter, r *http.Request, status int, msg string)
	logger      *slog.Logger

	mu           sync.Mutex
	visitors     map[string]*visitor
	overflow     *visitor // seau unique des clés qui ne tiennent plus dans la map
	lastSweep    time.Time
	lastOverflow time.Time // dernier Warn de débordement
}

// NewLimiter crée un limiteur : `r` requêtes/s en régime établi, rafale `burst`, par clé.
// name identifie le limiteur dans les logs et la métrique (jamais l'IP ni la clé).
func NewLimiter(name string, key func(*http.Request) string, r rate.Limit, burst int) *Limiter {
	return &Limiter{
		name: name, key: key, r: r, burst: burst,
		now: time.Now, maxVisitors: maxVisitors, reject: Error,
		visitors: map[string]*visitor{},
	}
}

// OnLimit enregistre un hook appelé à chaque refus (pour une métrique) ; nil = aucun.
func (l *Limiter) OnLimit(fn func(name string)) *Limiter { l.onLimit = fn; return l }

// Reject remplace le rendu du 429 (page d'erreur stylée) ; nil = httpx.Error.
func (l *Limiter) Reject(fn func(w http.ResponseWriter, r *http.Request, status int, msg string)) *Limiter {
	if fn == nil {
		fn = Error
	}
	l.reject = fn
	return l
}

// Clock remplace l'horloge du limiteur (tests) ; nil = time.Now.
func (l *Limiter) Clock(now func() time.Time) *Limiter {
	if now != nil {
		l.now = now
	}
	return l
}

// Logger fixe le logger des avertissements de saturation (défaut slog.Default()).
func (l *Limiter) Logger(lg *slog.Logger) *Limiter { l.logger = lg; return l }

// Name retourne le nom du limiteur.
func (l *Limiter) Name() string { return l.name }

// Reservation est un jeton pris dans un seau ; Cancel le rend (échec de validation
// qui ne doit pas coûter de quota).
type Reservation struct {
	ok    bool
	retry time.Duration
	res   *rate.Reservation
	now   time.Time
}

// OK indique si la requête est autorisée.
func (r Reservation) OK() bool { return r.ok }

// RetryAfter est le délai avant le prochain jeton quand OK() est faux.
func (r Reservation) RetryAfter() time.Duration { return r.retry }

// Cancel rend le jeton (sans effet si la réservation a été refusée).
func (r Reservation) Cancel() {
	if r.ok && r.res != nil {
		r.res.CancelAt(r.now)
	}
}

// Reserve prend un jeton dans le seau de la clé k. Un refus est comptabilisé (hook).
func (l *Limiter) Reserve(k string) Reservation {
	l.mu.Lock()
	t := l.now()
	v := l.visitorLocked(k, t)
	l.mu.Unlock()

	res := v.lim.ReserveN(t, 1)
	if !res.OK() {
		return l.refused(time.Second)
	}
	if d := res.DelayFrom(t); d > 0 {
		res.CancelAt(t)
		return l.refused(d)
	}
	return Reservation{ok: true, res: res, now: t}
}

func (l *Limiter) refused(d time.Duration) Reservation {
	if l.onLimit != nil {
		l.onLimit(l.name)
	}
	return Reservation{retry: d}
}

// AllowKey consomme un jeton pour la clé k.
func (l *Limiter) AllowKey(k string) (ok bool, retryAfter time.Duration) {
	res := l.Reserve(k)
	return res.ok, res.retry
}

// Allow consomme un jeton pour la clé de la requête.
func (l *Limiter) Allow(r *http.Request) (ok bool, retryAfter time.Duration) {
	return l.AllowKey(l.key(r))
}

// visitorLocked retourne le seau de la clé (créé au besoin) ; l.mu doit être tenu.
func (l *Limiter) visitorLocked(k string, t time.Time) *visitor {
	if t.Sub(l.lastSweep) > sweepEvery {
		l.sweepLocked(t)
	}
	if v, ok := l.visitors[k]; ok {
		v.seen = t
		return v
	}
	if len(l.visitors) >= l.maxVisitors && t.Sub(l.lastSweep) >= forcedSweepEvery {
		l.sweepLocked(t)
	}
	if len(l.visitors) >= l.maxVisitors {
		if t.Sub(l.lastOverflow) >= overflowWarnEvery {
			l.lastOverflow = t
			l.log().Warn("limiteur saturé : seau de débordement utilisé", slog.String("limiter", l.name))
		}
		if l.overflow == nil {
			l.overflow = &visitor{lim: rate.NewLimiter(l.r, l.burst)}
		}
		l.overflow.seen = t
		return l.overflow
	}
	v := &visitor{lim: rate.NewLimiter(l.r, l.burst), seen: t}
	l.visitors[k] = v
	return v
}

// sweepLocked évince les clés inactives, ainsi que celles dont le seau est de nouveau
// plein (les oublier est sans perte : un seau neuf est identique).
func (l *Limiter) sweepLocked(t time.Time) {
	for k, v := range l.visitors {
		if t.Sub(v.seen) > visitorIdle || v.lim.TokensAt(t) >= float64(l.burst) {
			delete(l.visitors, k)
		}
	}
	l.lastSweep = t
}

func (l *Limiter) log() *slog.Logger {
	if l.logger != nil {
		return l.logger
	}
	return slog.Default()
}

// Middleware applique le limiteur : dépassement → 429 + Retry-After.
func (l *Limiter) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, retry := l.Allow(r); !ok {
				l.Deny(w, r, retry, "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Deny répond 429 avec Retry-After (secondes, arrondi au supérieur, min 1). msg vide =
// message générique indiquant le délai.
func (l *Limiter) Deny(w http.ResponseWriter, r *http.Request, retry time.Duration, msg string) {
	secs := RetrySeconds(retry)
	w.Header().Set("Retry-After", fmt.Sprint(secs))
	if msg == "" {
		msg = fmt.Sprintf("Trop de requêtes, réessayez dans %d s.", secs)
	}
	l.reject(w, r, http.StatusTooManyRequests, msg)
}

// RetrySeconds arrondit un délai à la seconde supérieure (au moins 1).
func RetrySeconds(d time.Duration) int {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		return 1
	}
	return s
}

// tooManyFragment est la réponse 429 d'une requête htmx : une alerte à insérer, pas une page.
func tooManyFragment(msg string) string {
	return `<div class="alert alert-warning" role="alert"><span>` + html.EscapeString(msg) + `</span></div>`
}
