// Package web contient les handlers HTTP et le routage.
package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"doctolibre/internal/config"
	"doctolibre/internal/httpx"
	"doctolibre/internal/metrics"
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
	Metrics *metrics.Metrics
}

const pingTimeout = 2 * time.Second

// Routes construit le handler complet : routes + middlewares.
func Routes(d Deps) http.Handler {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", d.healthz)
	mux.HandleFunc("GET /{$}", index)

	var root http.Handler = mux
	if d.Metrics != nil {
		if d.Config.MetricsToken != "" {
			mux.Handle("GET /metrics", d.Metrics.Handler(d.Config.MetricsToken))
		}
		root = d.Metrics.Middleware(mux)
	}

	return httpx.Chain(root,
		httpx.RequestID(logger),
		httpx.Logger(),
		httpx.Recover(),
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

func index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(w, "Doctolibre")
}
