// Package testutil regroupe les aides de test (base isolée, horloge figée).
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/store"
)

const (
	envURL     = "DOCTOLIBRE_TEST_DATABASE_URL"
	defaultURL = "postgres://doctolibre:doctolibre@localhost:5432/doctolibre_test?sslmode=disable" //nolint:gosec // identifiants du compose de dev
	opTimeout  = 30 * time.Second
	extLockID  = 0x646f63746f // clé du verrou advisory de création des extensions
)

var (
	extOnce sync.Once
	extErr  error
)

// DB retourne un pool isolé dans un schéma dédié test_<id>, migré, supprimé à
// la fin du test. Skippe le test si Postgres n'est pas joignable.
func DB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv(envURL)
	if url == "" {
		url = defaultURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("testutil.DB: %s invalide: %v", envURL, err)
	}

	admin, err := store.Open(ctx, url)
	if err != nil {
		t.Skipf("testutil.DB: postgres indisponible (%s) : %v", envURL, err)
	}
	t.Cleanup(admin.Close)

	// Extensions créées une fois dans public ; la migration 00001 devient un no-op.
	extOnce.Do(func() {
		// Verrou advisory transactionnel : sync.Once ne protège pas entre les
		// process (un par package go test), CREATE EXTENSION concurrent peut échouer en 23505.
		tx, err := admin.Begin(ctx)
		if err != nil {
			extErr = err
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", extLockID); err != nil {
			extErr = err
			return
		}
		for _, e := range []string{"btree_gist", "citext"} {
			if _, err := tx.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS "+e+" WITH SCHEMA public"); err != nil {
				extErr = err
				return
			}
		}
		extErr = tx.Commit(ctx)
	})
	if extErr != nil {
		t.Fatalf("testutil.DB: extensions: %v", extErr)
	}

	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("testutil.DB: rand: %v", err)
	}
	schema := "test_" + hex.EncodeToString(b[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatalf("testutil.DB: create schema: %v", err)
	}

	// Pool propre au test : search_path fixé à la connexion, jamais de SET partagé.
	cfg.ConnConfig.RuntimeParams["search_path"] = quoted + ", public"
	cfg.MaxConns = 5
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("testutil.DB: pool: %v", err)
	}

	// Cleanups LIFO : le drop (enregistré en dernier de ceux-ci) passe avant admin.Close.
	t.Cleanup(func() {
		pool.Close()
		cctx, ccancel := context.WithTimeout(context.Background(), opTimeout)
		defer ccancel()
		if _, err := admin.Exec(cctx, "DROP SCHEMA IF EXISTS "+quoted+" CASCADE"); err != nil {
			t.Errorf("testutil.DB: drop schema %s: %v", schema, err)
		}
	})

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("testutil.DB: migrate: %v", err)
	}
	return pool
}
