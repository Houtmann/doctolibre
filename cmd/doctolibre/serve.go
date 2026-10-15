package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/metrics"
	"doctolibre/internal/store"
	"doctolibre/internal/web"
)

// serve lance le serveur HTTP jusqu'à l'annulation de ctx.
func serve(ctx context.Context, logOut io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	logger := newLogger(cfg, logOut)
	deps := web.Deps{Config: cfg, Logger: logger}
	if cfg.DatabaseURL != "" {
		pool, err := store.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("base de données: %w", err)
		}
		defer pool.Close()
		deps.DB = pool
		deps.Auth = &auth.Service{Pool: pool, Clock: domain.SystemClock{}, Logger: logger, Secure: cfg.IsProd()}
	} else {
		logger.Warn("DOCTOLIBRE_DATABASE_URL vide : l'espace /admin est désactivé")
	}
	if cfg.MetricsToken != "" {
		deps.Metrics = metrics.New()
	}
	handler := web.Routes(deps)
	return httpx.Run(ctx, httpx.New(cfg, handler), logger)
}

// newLogger : JSON en prod, texte lisible en dev.
func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	if cfg.IsProd() {
		return slog.New(slog.NewJSONHandler(w, nil))
	}
	return slog.New(slog.NewTextHandler(w, nil))
}
