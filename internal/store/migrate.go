package store

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"doctolibre/migrations"
)

func newProvider(pool *pgxpool.Pool) (*goose.Provider, error) {
	db := stdlib.OpenDBFromPool(pool)
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return nil, fmt.Errorf("goose provider: %w", err)
	}
	return p, nil
}

// Migrate applique toutes les migrations en attente.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	p, err := newProvider(pool)
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// MigrateDown annule la dernière migration appliquée.
func MigrateDown(ctx context.Context, pool *pgxpool.Pool) error {
	p, err := newProvider(pool)
	if err != nil {
		return err
	}
	if _, err := p.Down(ctx); err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

// MigrateStatus écrit l'état de chaque migration sur w.
func MigrateStatus(ctx context.Context, pool *pgxpool.Pool, w io.Writer) error {
	p, err := newProvider(pool)
	if err != nil {
		return err
	}
	st, err := p.Status(ctx)
	if err != nil {
		return fmt.Errorf("migrate status: %w", err)
	}
	for _, s := range st {
		state := "pending"
		if s.State == goose.StateApplied {
			state = "applied " + s.AppliedAt.UTC().Format("2006-01-02 15:04:05Z")
		}
		if _, err := fmt.Fprintf(w, "%05d %-24s %s\n", s.Source.Version, s.Source.Path, state); err != nil {
			return err
		}
	}
	return nil
}

// MigrationVersion retourne la version de la dernière migration appliquée.
func MigrationVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	p, err := newProvider(pool)
	if err != nil {
		return 0, err
	}
	v, err := p.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("migration version: %w", err)
	}
	return v, nil
}
