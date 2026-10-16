package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/metrics"
	"doctolibre/internal/outbox"
	"doctolibre/internal/service"
	"doctolibre/internal/store"
	"doctolibre/internal/web"
)

// serve lance le serveur HTTP (et le worker outbox, sauf DOCTOLIBRE_WORKER=off)
// jusqu'à l'annulation de ctx.
func serve(ctx context.Context, logOut io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	logger := newLogger(cfg, logOut)
	deps := web.Deps{Config: cfg, Logger: logger}
	var pool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		pool, err = store.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("base de données: %w", err)
		}
		defer pool.Close()
		if cfg.AutoMigrate {
			if err := store.Migrate(ctx, pool); err != nil {
				return fmt.Errorf("migrations: %w", err)
			}
		}
		deps.DB = pool
		deps.Auth = &auth.Service{Pool: pool, Clock: domain.SystemClock{}, Logger: logger, Secure: cfg.IsProd()}
		deps.Messages = outbox.Queue{Pool: pool, Clock: domain.SystemClock{}}
		deps.Practice = &service.Practice{Pool: pool, Clock: domain.SystemClock{}}
		deps.Privacy = &service.Privacy{Pool: pool, Clock: domain.SystemClock{}, Logger: logger}
		deps.Audit = &service.Audit{Pool: pool, Clock: domain.SystemClock{}}
		deps.Feed = &service.CalendarFeed{Pool: pool, Clock: domain.SystemClock{}}
		deps.Calendar = &service.Calendar{Pool: pool, Clock: domain.SystemClock{}, Events: service.NopSink{}, BaseURL: cfg.BaseURL}
		deps.Patients = &service.Patients{Pool: pool, Clock: domain.SystemClock{}}
		deps.CarePlans = &service.CarePlans{Pool: pool, Clock: domain.SystemClock{}, Events: service.NopSink{}, BaseURL: cfg.BaseURL}
		deps.Booking = &service.Booking{Pool: pool, Clock: domain.SystemClock{}, OTP: service.OutboxOTPSender{}, BaseURL: cfg.BaseURL}
		deps.Status = &service.Status{Pool: pool, Clock: domain.SystemClock{}, Version: version, StartedAt: domain.SystemClock{}.Now(), BackupDir: cfg.BackupDir}
		deps.Waitlist = &service.Waitlist{Pool: pool, Clock: domain.SystemClock{}, OTP: service.OutboxOTPSender{}, BaseURL: cfg.BaseURL}
	} else {
		logger.Warn("DOCTOLIBRE_DATABASE_URL vide : l'espace /admin est désactivé")
	}
	if cfg.MetricsToken != "" {
		deps.Metrics = metrics.New()
		if pool != nil {
			if err := registerDBMetrics(deps.Metrics, pool); err != nil {
				return err
			}
			created := func(source string) { deps.Metrics.AppointmentsCreated.WithLabelValues(source).Inc() }
			deps.Booking.OnCreated = created
			deps.Calendar.OnCreated = created
		}
	}
	dispatcher, err := buildDispatcher(cfg, logger)
	if err != nil {
		return fmt.Errorf("notifications: %w", err)
	}
	handler := web.Routes(deps)

	// Le worker partage le contexte du serveur : un signal arrête les deux. On
	// attend la fin du worker avant de fermer le pool (defer pool.Close).
	_, waitWorker := startWorker(ctx, cfg, pool, dispatcher, logger)
	err = httpx.Run(ctx, httpx.New(cfg, handler), logger)
	stop() // erreur HTTP sans signal : arrêter aussi le worker
	waitWorker()
	logger.Info("arrêt complet")
	return err
}

// registerDBMetrics branche les métriques de l'outbox et du pool de connexions.
func registerDBMetrics(m *metrics.Metrics, pool *pgxpool.Pool) error {
	if err := m.RegisterOutbox(outbox.Queue{Pool: pool, Clock: domain.SystemClock{}}); err != nil {
		return fmt.Errorf("métriques outbox: %w", err)
	}
	err := m.RegisterPool(func() metrics.PoolStats {
		s := pool.Stat()
		return metrics.PoolStats{Acquired: s.AcquiredConns(), Idle: s.IdleConns(), Total: s.TotalConns(), Max: s.MaxConns()}
	})
	if err != nil {
		return fmt.Errorf("métriques pool: %w", err)
	}
	return nil
}

// newLogger : JSON en prod, texte lisible en dev.
func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	if cfg.IsProd() {
		return slog.New(slog.NewJSONHandler(w, nil))
	}
	return slog.New(slog.NewTextHandler(w, nil))
}
