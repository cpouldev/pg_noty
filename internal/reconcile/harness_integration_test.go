//go:build integration

package reconcile

import (
	"context"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/testpostgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type reconcileRunner interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// skipIfShort is the internal/source/testmain_integration_test.go:skipIfShort twin. The difference
// is the package under test and nothing else.
func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping the container-backed tier under -short")
	}
}

// harnessConfig is the internal/source/testmain_integration_test.go:harnessConfig twin. The
// difference is the package under test and nothing else.
func harnessConfig(t *testing.T) config.Config {
	t.Helper()
	skipIfShort(t)
	dsn, err := sharedContainer.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatalf("read container connection string: %v", err)
	}
	return config.Config{Instance: harnessSchema, Database: config.Database{URL: dsn, Schema: harnessSchema}}
}

// restoreToSnapshot is the internal/source/testmain_integration_test.go:restoreToSnapshot twin.
// The difference is the package under test and nothing else.
func restoreToSnapshot(t *testing.T) {
	t.Helper()
	skipIfShort(t)
	if err := testpostgres.RestoreSnapshot(t.Context(), sharedContainer); err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}
}

// freshDatabase is the internal/source/testmain_integration_test.go:freshDatabase twin. The
// difference is the package under test and nothing else.
func freshDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	restoreToSnapshot(t)
	pool, err := pgxpool.New(t.Context(), harnessConfig(t).Database.URL)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// mustExecOn is the internal/source/harness_integration_test.go:mustExecOn twin. The difference is
// the package under test and nothing else.
func mustExecOn(t *testing.T, on reconcileRunner, statement string, arguments ...any) {
	t.Helper()
	if _, err := on.Exec(t.Context(), statement, arguments...); err != nil {
		t.Fatalf("exec %s: %v", statement, err)
	}
}

// reconcileCounter is countOn's subject: a pool for a committed reading, or the transaction under
// test for an uncommitted one. A pool cannot see another transaction's uncommitted rows, so a
// rollback assertion taken only on the pool is satisfied by a writer that wrote nothing.
type reconcileCounter interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// countOn is the internal/source/harness_integration_test.go:countOn twin, widened from *pgxpool.Pool
// to reconcileCounter so a caller can count inside the transaction it is about to roll back rather
// than declare a second reader for it. The package under test and that widening are the only
// differences.
func countOn(t *testing.T, on reconcileCounter, query string, arguments ...any) int {
	t.Helper()
	var count int
	if err := on.QueryRow(t.Context(), query, arguments...).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", query, err)
	}
	return count
}

// harnessLiteral quotes values in SQL statements used by these integration tests.
func harnessLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
