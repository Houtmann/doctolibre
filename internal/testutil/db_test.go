package testutil_test

import (
	"context"
	"testing"
	"time"

	"doctolibre/internal/testutil"
)

func TestDBIsolation(t *testing.T) {
	for _, name := range []string{"a", "b", "c"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.DB(t)
			ctx := context.Background()
			// Échouerait ("already exists") si le schéma était partagé.
			if _, err := db.Exec(ctx, `CREATE TABLE marker (name text)`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `INSERT INTO marker VALUES ($1)`, name); err != nil {
				t.Fatal(err)
			}
			var n int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM marker`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Errorf("marker: %d lignes, attendu 1", n)
			}
			var tableSchema, cur string
			if err := db.QueryRow(ctx, `SELECT current_schema()`).Scan(&cur); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(ctx, `SELECT schemaname FROM pg_tables WHERE tablename='goose_db_version' AND schemaname = $1`, cur).Scan(&tableSchema); err != nil {
				t.Fatalf("goose_db_version absente du schéma du test %q: %v", cur, err)
			}
			if cur == "public" {
				t.Error("schéma courant = public")
			}
		})
	}
}

// Le schéma d'un test doit avoir disparu une fois le test terminé.
func TestSchemaDroppedAfterTest(t *testing.T) {
	var schema string
	t.Run("inner", func(t *testing.T) {
		db := testutil.DB(t)
		if err := db.QueryRow(context.Background(), `SELECT current_schema()`).Scan(&schema); err != nil {
			t.Fatal(err)
		}
	})
	db := testutil.DB(t)
	var n int
	if err := db.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_namespace WHERE nspname = $1`, schema).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("schéma %s toujours présent", schema)
	}
}

func TestFixedClock(t *testing.T) {
	t.Parallel()
	start := time.Date(2025, 3, 30, 0, 0, 0, 0, time.UTC)
	c := testutil.NewFixedClock(start)
	c.Advance(time.Hour)
	if !c.Now().Equal(start.Add(time.Hour)) {
		t.Fatal("Advance sans effet")
	}
}
