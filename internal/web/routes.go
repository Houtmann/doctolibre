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

	Renderer *Renderer // optionnel : nil = construit depuis Config
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
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { rd.Error(w, r, http.StatusNotFound, "") })

	return httpx.Chain(mux,
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
