package store_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"doctolibre/internal/store"
	"doctolibre/internal/testutil"
)

func TestMigrateStatus(t *testing.T) {
	t.Parallel()
	pool := testutil.DB(t) // déjà migrée
	var out bytes.Buffer
	if err := store.MigrateStatus(context.Background(), pool, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "00001_init.sql") {
		t.Errorf("status sans migration init: %q", out.String())
	}
}
