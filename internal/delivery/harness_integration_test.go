//go:build integration

package delivery

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func applyDeliveryMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "schema", "migrations", "*.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("read migrations: %v", err)
	}
	sort.Strings(paths)
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(t.Context(), string(body)); err != nil {
			t.Fatalf("apply %s: %v", path, err)
		}
	}
}

func TestDeliveryHarnessStartsDisposableDatabase(t *testing.T) {
	pool := deliveryPool(t)
	var migrated bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass(current_schema() || '.schema_version') IS NOT NULL").Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if !migrated {
		applyDeliveryMigrations(t, pool)
	}
	var database string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	if database != deliveryDatabase {
		t.Fatalf("database=%q, want %q", database, deliveryDatabase)
	}
}

func TestDeliveryHarnessUsesPerProcessResourceNames(t *testing.T) {
	if !strings.Contains(deliveryDatabase, "delivery_"+itoa(os.Getpid())) {
		t.Fatalf("database %q is not process-scoped", deliveryDatabase)
	}
	if strings.Contains(deliveryDatabase, ":5432") {
		t.Fatalf("database name contains a fixed host port: %q", deliveryDatabase)
	}
}

func itoa(value int) string {
	return fmt.Sprintf("%d", value)
}
