package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/outbox"
	"doctolibre/internal/service"
	"doctolibre/internal/store"
)

// startWorker lance outbox.Worker.Run dans une goroutine, sauf si le worker est
// désactivé (DOCTOLIBRE_WORKER=off). wait() bloque jusqu'à la fin du worker
// (après annulation de ctx) ; il est toujours appelable.
func startWorker(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, d outbox.Dispatcher, logger *slog.Logger) (started bool, wait func()) {
	if !cfg.WorkerEnabled || pool == nil {
		logger.Info("worker outbox désactivé")
		return false, func() {}
	}
	w := &outbox.Worker{Pool: pool, Dispatcher: d, Clock: domain.SystemClock{}, Logger: logger}
	var wg sync.WaitGroup
	wg.Add(5)
	// Expiration des inscriptions en liste d'attente dont la fenêtre est passée.
	wl := &service.Waitlist{Pool: pool, Clock: domain.SystemClock{}, Logger: logger}
	go func() {
		defer wg.Done()
		if err := wl.Run(ctx); err != nil {
			logger.Error("liste d'attente: expiration en échec", "error", err)
		}
	}()
	// Rappels de rendez-vous : enfilés peu avant l'envoi (opt-out relu à ce moment).
	rem := &service.Reminders{Pool: pool, Clock: domain.SystemClock{}, Logger: logger}
	go func() {
		defer wg.Done()
		if err := rem.Run(ctx); err != nil {
			logger.Error("rappels en échec", "error", err)
		}
	}()
	// Alertes aux admins (messages en échec, sauvegarde absente), dédoublonnées par fenêtre.
	alerts := &service.Alerts{Pool: pool, Clock: domain.SystemClock{}, Logger: logger, BackupDir: cfg.BackupDir, BaseURL: cfg.BaseURL}
	go func() {
		defer wg.Done()
		if err := alerts.Run(ctx); err != nil {
			logger.Error("alertes en échec", "error", err)
		}
	}()
	// Purge RGPD quotidienne : contenu des messages, sessions et codes expirés, journal, patients inactifs.
	purge := &service.Privacy{Pool: pool, Clock: domain.SystemClock{}, Logger: logger}
	go func() {
		defer wg.Done()
		if err := purge.Run(ctx); err != nil {
			logger.Error("purge RGPD en échec", "error", err)
		}
	}()
	go func() {
		defer wg.Done()
		logger.Info("worker outbox démarré")
		if err := w.Run(ctx); err != nil {
			logger.Error("worker outbox en échec", "error", err)
		}
		logger.Info("worker outbox arrêté")
	}()
	return true, wg.Wait
}

// runWorker : sous-commande `doctolibre worker`, le worker seul sans HTTP.
func runWorker(ctx context.Context, logOut io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	logger := newLogger(cfg, logOut)
	if cfg.DatabaseURL == "" {
		return errors.New("DOCTOLIBRE_DATABASE_URL est vide")
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("base de données: %w", err)
	}
	defer pool.Close()
	cfg.WorkerEnabled = true // la sous-commande est explicite
	dispatcher, err := buildDispatcher(cfg, logger)
	if err != nil {
		return fmt.Errorf("notifications: %w", err)
	}
	_, wait := startWorker(ctx, cfg, pool, dispatcher, logger)
	wait()
	return nil
}
