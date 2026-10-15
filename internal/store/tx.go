package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/store/sqlcgen"
)

// WithTx exécute fn dans une transaction : commit si fn réussit, rollback sinon.
// Aucun appel réseau (SMTP, SMS) dans fn.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(q *sqlcgen.Queries, tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op après commit
	if err := fn(sqlcgen.New(tx), tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
