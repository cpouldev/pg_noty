//go:build integration

package reconcile

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
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
	containerUses.Add(1)
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
	containerUses.Add(1)
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

// applySchemaMigrations is the internal/source/harness_integration_test.go:applySchemaMigrations
// twin. The difference is the package under test and nothing else.
func applySchemaMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "schema", "migrations", "*.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("read internal/schema migration corpus: %v", err)
	}
	slices.Sort(paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		mustExecOn(t, pool, string(data))
	}
}

// asRole is the internal/source/harness_integration_test.go:asRole twin. The difference is the
// package under test and nothing else.
func asRole(t *testing.T, cfg config.Config, role, password string) config.Config {
	t.Helper()
	parsed, err := url.Parse(cfg.Database.URL)
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}
	parsed.User = url.UserPassword(role, password)
	cfg.Database.URL = parsed.String()
	return cfg
}

// openPoolAs is the internal/source/harness_integration_test.go:openPoolAs twin. The difference is
// the package under test and nothing else.
func openPoolAs(t *testing.T, cfg config.Config, role string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open pool as %s: %v", role, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// createTheLimitedRole is the internal/source/harness_integration_test.go:createTheLimitedRole twin.
// The difference is the package under test and nothing else.
func createTheLimitedRole(t *testing.T, pool *pgxpool.Pool, role, password string) {
	t.Helper()
	quoted, fault := schema.Quoted(role)
	if fault != schema.IdentifierOK {
		t.Fatalf("limited role %s is unusable: %s", role, fault)
	}
	mustExecOn(t, pool, "CREATE ROLE "+quoted+" LOGIN PASSWORD "+harnessLiteral(password))
}

// harnessLiteral is a local harness routine for createTheLimitedRole, whose source twin is
// internal/source/harness_integration_test.go:createTheLimitedRole. It cannot reuse source's
// unexported literal authority, internal/source/literal.go:quoteLiteral; harnessPassword has no
// backslashes, so plain PostgreSQL quote escaping is sufficient.
func harnessLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func TestExactlyOneContainerServesTheWholePackage(t *testing.T) {
	skipIfShort(t)
	if got := containersStarted.Load(); got != 1 {
		t.Fatalf("containers started = %d, want 1", got)
	}
	if containerStartup <= 0 {
		t.Fatal("container startup was not measured")
	}
}
