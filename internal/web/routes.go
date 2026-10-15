// Package web contient les handlers HTTP et le routage.
package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/httpx"
	"doctolibre/internal/metrics"
	"doctolibre/internal/service"
)

// Pinger vérifie la disponibilité d'une dépendance (ex. *pgxpool.Pool).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps regroupe les dépendances des handlers.
type Deps struct {
	Config config.Config
	Logger *slog.Logger
	DB     Pinger // optionnel : nil = pas de ping DB dans /healthz
	// Metrics est optionnel. /metrics n'est exposé que si Metrics != nil ET
	// Config.MetricsToken != "" ; la mesure des requêtes ne dépend que de Metrics.
	Metrics  *metrics.Metrics
	Renderer *Renderer // optionnel : nil = construit depuis Config
	// Auth est optionnel : nil = pas de routes /admin (pas de base de données).
	Auth *auth.Service
	// Messages est optionnel : nil = pas de page /admin/messages.
	Messages MessageQueue
	// Practice (service de paramétrage du cabinet) est optionnel : nil = pas de pages de configuration.
	Practice *service.Practice
	// Calendar (agenda du praticien) est optionnel : nil = pas de pages d'agenda.
	Calendar *service.Calendar
	// Patients (fiches patients) est optionnel : nil = pas de pages /admin/patients.
	Patients *service.Patients
	// Booking (réservation en ligne des patients) est optionnel : nil = pas de pages /p/… ni /r/….
	Booking *service.Booking
	// Feed (flux ICS du praticien) est optionnel : nil = pas de /ics/… ni de réglage d'abonnement.
	Feed *service.CalendarFeed
}

const pingTimeout = 2 * time.Second

// Routes construit le handler complet : routes + middlewares.
func Routes(d Deps) http.Handler {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	rd := d.Renderer
	if rd == nil {
		var err error
		if rd, err = d.newRenderer(); err != nil {
			logger.Error("templates: initialisation en échec", slog.String("err", err.Error()))
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", d.healthz)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { rd.Page(w, r, "home", nil) })
	if rd != nil {
		mux.Handle("GET /static/", rd.StaticHandler())
	}
	if d.Auth != nil && rd != nil {
		d.adminRoutes(mux, rd, logger)
	}
	if d.Booking != nil && rd != nil {
		d.publicRoutes(mux, rd, logger)
	}
	if d.Feed != nil && rd != nil {
		d.feedRoutes(mux, rd, logger)
	}
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { rd.Error(w, r, http.StatusNotFound, "") })

	var root http.Handler = mux
	if d.Metrics != nil {
		if d.Config.MetricsToken != "" {
			mux.Handle("GET /metrics", d.Metrics.Handler(d.Config.MetricsToken))
		}
		root = d.Metrics.Middleware(mux)
	}

	return httpx.Chain(root,
		httpx.TrustedProxies(d.Config.TrustedProxies),
		httpx.RequestID(logger),
		httpx.Logger(),
		httpx.RecoverWith(rd.Error),
		httpx.SecureHeaders(d.Config.IsProd()),
		httpx.CSRF(d.Config.IsProd()),
	)
}

func (d Deps) healthz(w http.ResponseWriter, r *http.Request) {
	if d.DB != nil {
		ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			httpx.LoggerFrom(r.Context()).Error("healthz: ping DB en échec", slog.String("err", err.Error()))
			httpx.Error(w, r, http.StatusServiceUnavailable, "base de données indisponible")
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprint(w, "ok")
}

func (d Deps) newRenderer() (*Renderer, error) {
	loc, err := time.LoadLocation(d.Config.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return NewRenderer(RendererOptions{
		SecretKey: d.Config.SecretKey,
		Location:  loc,
		Reload:    !d.Config.IsProd(),
		Secure:    d.Config.IsProd(),
	})
}
