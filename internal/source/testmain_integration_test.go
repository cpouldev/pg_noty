//go:build integration

package source

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	harnessImage     = "postgres:17.10-alpine"
	harnessDatabase  = "notyharness"
	harnessUser      = "noty"
	harnessPassword  = "noty-harness-secret"
	harnessSchema    = "noty"
	harnessSQLDriver = "pgx"
)

var (
	sharedContainer   *postgres.PostgresContainer
	containersStarted atomic.Int64
)

func TestMain(m *testing.M) { os.Exit(runSourceHarness(m)) }

func runSourceHarness(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}
	ctx := context.Background()
	container, err := startSourceContainer(ctx)
	if container != nil {
		defer container.Terminate(ctx)
	}
	if err != nil {
		return sourceHarnessRefusal("start container: %v", err)
	}
	sharedContainer = container
	if err := container.Snapshot(ctx); err != nil {
		return sourceHarnessRefusal("snapshot database: %v", err)
	}
	code := m.Run()
	if code != 0 {
		return code
	}
	if containersStarted.Load() != 1 {
		return sourceHarnessRefusal("started %d containers, want 1", containersStarted.Load())
	}
	return 0
}

func startSourceContainer(ctx context.Context) (*postgres.PostgresContainer, error) {
	containersStarted.Add(1)
	return postgres.Run(
		ctx,
		harnessImage,
		postgres.WithDatabase(harnessDatabase),
		postgres.WithUsername(harnessUser),
		postgres.WithPassword(harnessPassword),
		postgres.WithSQLDriver(harnessSQLDriver),
		postgres.BasicWaitStrategies(),
	)
}

// reportToHarnessStderr says something about this run where a reader will see it without -v.
// TestMain's refusals and skipTheUnmeasuredCeiling's report share it rather than each opening their
// own writer; only the former is an exit code.
func reportToHarnessStderr(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "source harness: "+format+"\n", args...)
}

func sourceHarnessRefusal(format string, args ...any) int {
	reportToHarnessStderr(format, args...)
	return 1
}

func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping the container-backed tier under -short")
	}
}

func harnessConfig(t *testing.T) config.Config {
	t.Helper()
	skipIfShort(t)
	dsn, err := sharedContainer.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatalf("read container connection string: %v", err)
	}
	return config.Config{Instance: harnessSchema, Database: config.Database{URL: dsn, Schema: harnessSchema}}
}

func restoreToSnapshot(t *testing.T) {
	t.Helper()
	skipIfShort(t)
	if err := testpostgres.RestoreSnapshot(t.Context(), sharedContainer); err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}
}

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
