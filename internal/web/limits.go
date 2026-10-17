package web

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"doctolibre/internal/auth"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

// Quotas du back-office (par IP avant l'authentification, par utilisateur après).
const (
	adminIPRate     = rate.Limit(20)
	adminIPBurst    = 200
	adminUserRate   = rate.Limit(5)
	adminUserBurst  = 100
	adminHeavyBurst = 10
	// adminHeavyEvery : 1 requête / 2 s en régime établi sur les routes coûteuses.
	adminHeavyEvery = 2 * time.Second
)

// Quotas de la réservation en ligne (par IP, sauf mention).
const (
	browseRate  = rate.Limit(2)
	browseBurst = 30
	// maxOnlineBookingsPerHour borne les réservations en ligne par praticien, quel que
	// soit le nombre d'IP et d'adresses : au-delà, il faut appeler le cabinet.
	maxOnlineBookingsPerHour = 30
	// bookingWarnEvery : au plus un Warn par praticien et par heure.
	bookingWarnEvery = time.Hour
)

// heavyAdminRoutes : routes coûteuses (import, exports, JSON de l'agenda, renvoi de
// message, effacement), soumises en plus au limiteur admin-heavy. Clé = r.Pattern.
// Une seule liste, ici, plutôt qu'une annotation par route.
var heavyAdminRoutes = map[string]bool{
	"POST " + pathPatientImport:                 true, // aperçu et import CSV
	"GET " + pathPatients + "/{id}/export.json": true, // export RGPD
	"GET " + pathPatients + "/{id}/export.csv":  true,
	"POST " + pathPatients + "/{id}/delete":     true, // effacement RGPD
	"GET " + pathCalendarEvents:                 true, // JSON de l'agenda
	"POST /admin/messages/{id}/resend":          true,
}

// newLimiter crée un limiteur câblé sur la métrique, le logger et la page d'erreur stylée.
func (d Deps) newLimiter(rd *Renderer, logger *slog.Logger, name string, key func(*http.Request) string, r rate.Limit, burst int) *httpx.Limiter {
	l := httpx.NewLimiter(name, key, r, burst).Logger(logger).Clock(d.LimiterNow)
	if rd != nil {
		l.Reject(rd.Error)
	}
	if d.Metrics != nil {
		l.OnLimit(d.Metrics.RateLimited)
	}
	return l
}

// withAdminLimits retourne les dépendances dont Auth.RequireUser applique les limiteurs
// du back-office : admin-ip (avant l'authentification), puis admin-user (et admin-heavy
// sur les routes de heavyAdminRoutes) après. Toutes les routes protégées en héritent.
func (d Deps) withAdminLimits(rd *Renderer, logger *slog.Logger) Deps {
	byUser := func(r *http.Request) string {
		u, _ := auth.UserFrom(r.Context())
		return u.ID.String()
	}
	ip := d.newLimiter(rd, logger, "admin-ip", httpx.IPKey, adminIPRate, adminIPBurst)
	user := d.newLimiter(rd, logger, "admin-user", byUser, adminUserRate, adminUserBurst)
	heavy := d.newLimiter(rd, logger, "admin-heavy", byUser, rate.Every(adminHeavyEvery), adminHeavyBurst)
	post := func(next http.Handler) http.Handler {
		limited := user.Middleware()(next)
		heavied := user.Middleware()(heavy.Middleware()(next))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if heavyAdminRoutes[r.Pattern] {
				heavied.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
	d.Auth = d.Auth.WithLimits(ip.Middleware(), post)
	return d
}

// publicLimits regroupe les limiteurs partagés de la réservation en ligne : un seau
// par parcours (et non par route), donc un quota effectif unique par IP.
type publicLimits struct {
	browse       *httpx.Limiter // pages d'accueil du cabinet et du praticien
	search       *httpx.Limiter // grilles, premier créneau, formulaire, grille de déplacement
	book         *httpx.Limiter // POST de réservation et de liste d'attente
	manage       *httpx.Limiter // lien de gestion /r/{jeton}
	offer        *httpx.Limiter // lien de la liste d'attente /w/{jeton}
	practitioner *httpx.Limiter // réservations par praticien (Reserve/AllowKey uniquement, pas de clé de requête)
	warned       *hourlyWarn
}

func (d Deps) newPublicLimits(rd *Renderer, logger *slog.Logger) *publicLimits {
	perIP := rate.Every(publicInterval)
	return &publicLimits{
		browse: d.newLimiter(rd, logger, "public-browse", httpx.IPKey, browseRate, browseBurst),
		search: d.newLimiter(rd, logger, "public-search", httpx.IPKey, perIP, publicBurst),
		book:   d.newLimiter(rd, logger, "public-book", httpx.IPKey, perIP, publicBurst),
		manage: d.newLimiter(rd, logger, "public-manage", httpx.IPKey, perIP, publicBurst),
		offer:  d.newLimiter(rd, logger, "public-offer", httpx.IPKey, perIP, publicBurst),
		practitioner: d.newLimiter(rd, logger, "public-book-practitioner", nil,
			rate.Every(time.Hour/maxOnlineBookingsPerHour), maxOnlineBookingsPerHour),
		warned: &hourlyWarn{last: map[uuid.UUID]time.Time{}},
	}
}

// hourlyWarn limite à un message par praticien et par période. Les clés sont des IDs de
// praticiens existants : la map reste bornée par leur nombre.
type hourlyWarn struct {
	mu   sync.Mutex
	last map[uuid.UUID]time.Time
}

func (h *hourlyWarn) due(id uuid.UUID, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t, ok := h.last[id]; ok && now.Sub(t) < bookingWarnEvery {
		return false
	}
	h.last[id] = now
	return true
}

// emailKey : clé de limitation par adresse (casse et espaces ignorés, longueur bornée).
func emailKey(r *http.Request) string {
	k := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	if len(k) > maxEmailKeyLen {
		k = k[:maxEmailKeyLen]
	}
	return k
}

// reservePractitioner prend un jeton du quota horaire de réservations du praticien juste
// avant l'appel au service (pas en middleware : un OTP faux ou une saisie invalide ne doit
// pas consommer le quota, l'appelant fait Cancel dans ce cas). Refus : réponse rendue par
// deny avec un message invitant à appeler le cabinet, et ok=false.
func (h *publicHandlers) reservePractitioner(w http.ResponseWriter, r *http.Request, t service.Target, deny func(msg string)) (httpx.Reservation, bool) {
	res := h.lim.practitioner.Reserve(t.Practitioner.ID.String())
	if res.OK() {
		return res, true
	}
	if h.lim.warned.due(t.Practitioner.ID, h.svc.Now()) {
		httpx.LoggerFrom(r.Context()).Warn("quota de réservations en ligne atteint",
			slog.String("practitioner_id", t.Practitioner.ID.String()), slog.String("practitioner", t.Practitioner.Slug),
			slog.String("practice_id", t.Practice.ID.String()))
	}
	w.Header().Set("Retry-After", strconv.Itoa(httpx.RetrySeconds(res.RetryAfter())))
	msg := "Les réservations en ligne sont momentanément limitées pour ce praticien."
	if t.Practice.Phone != "" {
		msg += " Merci d'appeler le cabinet au " + t.Practice.Phone + "."
	} else {
		msg += " Merci de contacter le cabinet."
	}
	deny(msg)
	return res, false
}
