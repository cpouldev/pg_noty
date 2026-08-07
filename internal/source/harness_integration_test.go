//go:build integration

package source

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type sourceRunner interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Copy of internal/schema/harness_integration_test.go:mustExecOn. Source cannot import that
// unexported test symbol, so the duplication is forced and recorded here.
func mustExecOn(t *testing.T, on sourceRunner, statement string, arguments ...any) {
	t.Helper()
	if _, err := on.Exec(t.Context(), statement, arguments...); err != nil {
		t.Fatalf("exec %s: %v", statement, err)
	}
}

// countOn is one scalar count, with the read's own error fatal. A discarded Scan leaves the counter
// at its zero value, and zero is the answer the absence tests read as success -- so an unreachable
// event log, a renamed table or a revoked SELECT would report "nothing was written" and pass the
// tests that exist to prove nothing was written.
func countOn(t *testing.T, pool *pgxpool.Pool, query string, arguments ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(t.Context(), query, arguments...).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", query, err)
	}
	return count
}

// Copy of internal/schema/grants_integration_test.go:asRole. The source harness owns its package's
// integration binary and therefore swaps credentials locally.
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

// Copy of internal/schema/grants_integration_test.go:openPoolAs.
func openPoolAs(t *testing.T, cfg config.Config, role string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open pool as %s: %v", role, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Copy of internal/schema/runner_integration_test.go:mustMigrate. The source side accepts SQL text
// from internal/schema's migration directory because schema's private migration types cannot cross
// packages.
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

// Copy of internal/schema/bootstrapprivilege_integration_test.go:createTheLimitedRole.
func createTheLimitedRole(t *testing.T, pool *pgxpool.Pool, role, password string) {
	t.Helper()
	quoted, fault := schema.Quoted(role)
	if fault != schema.IdentifierOK {
		t.Fatalf("limited role %s is unusable: %s", role, fault)
	}
	mustExecOn(t, pool, "CREATE ROLE "+quoted+" LOGIN PASSWORD "+quoteLiteral(password))
}

func TestExactlyOneContainerServesTheWholePackage(t *testing.T) {
	skipIfShort(t)
	if got := containersStarted.Load(); got != 1 {
		t.Fatalf("containers started = %d, want 1", got)
	}
	if containerStartup <= 0 {
		t.Fatal("container startup was not measured")
	}
}
