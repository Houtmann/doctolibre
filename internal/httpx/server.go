// Package httpx fournit le serveur HTTP, les middlewares et les helpers de réponse.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"doctolibre/internal/config"
)

// Timeouts du serveur.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// New construit un http.Server configuré (timeouts) pour la configuration donnée.
func New(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// Run sert jusqu'à l'annulation de ctx (typiquement signal.NotifyContext sur
// SIGINT/SIGTERM), puis arrête le serveur proprement (10 s max).
func Run(ctx context.Context, srv *http.Server, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("serveur HTTP démarré", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("serveur http: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	logger.Info("arrêt du serveur HTTP")
	shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("arrêt du serveur http: %w", err)
	}
	return nil
}
