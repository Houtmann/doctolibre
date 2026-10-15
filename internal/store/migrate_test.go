package store_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"doctolibre/internal/store"
)

// TestMigrateStatus nécessite une base dédiée (DOCTOLIBRE_TEST_DATABASE_URL) ;
// skippé sinon. Les helpers de base isolée arrivent avec T005.
func TestMigrateStatus(t *testing.T) {
	url := os.Getenv("DOCTOLIBRE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("DOCTOLIBRE_TEST_DATABASE_URL non défini")
	}
	ctx := context.Background()
	pool, err := store.Open(ctx, url)
	if err != nil {
		t.Skipf("postgres indisponible: %v", err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := store.MigrateStatus(ctx, pool, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "00001_init.sql") {
		t.Errorf("status sans migration init: %q", out.String())
	}
}
