//go:build integration

package source

import (
	"context"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
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
	containerUses     atomic.Int64
	containerStartup  time.Duration

	// harnessContainerRequest is the *testcontainers.GenericContainerRequest the sole postgres.Run
	// call below handed the library, recorded by recordingTheRequest and read back by
	// TestTheHarnessContainerRequestFixesNoHostPortAndNoContainerName. It is a live pointer, so it
	// holds the request as every customizer left it; testcontainers passes the request on by value,
	// so nothing after the customize loop can rewrite what this names. Written once from TestMain
	// before m.Run and only read afterwards.
	harnessContainerRequest any
)

// recordingTheRequest returns a customizer of option's own type that records the container request
// it is handed and then delegates to option.
//
// The type parameter is what keeps this file from naming testcontainers-go's types: importing that
// module directly would promote it from an indirect to a direct requirement of go.mod, which
// internal/config/releasehygiene_test.go refuses. The request therefore leaves as an any and is
// read by reflection in readContainerRequest.
func recordingTheRequest[Option any](option Option) Option {
	recorded := reflect.MakeFunc(
		reflect.TypeOf(option), func(arguments []reflect.Value) []reflect.Value {
			harnessContainerRequest = arguments[0].Interface()
			return reflect.ValueOf(option).Call(arguments)
		},
	)
	return recorded.Interface().(Option)
}

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
	started := time.Now()
	container, err := postgres.Run(
		ctx,
		harnessImage,
		postgres.WithDatabase(harnessDatabase),
		postgres.WithUsername(harnessUser),
		postgres.WithPassword(harnessPassword),
		postgres.WithSQLDriver(harnessSQLDriver),
		recordingTheRequest(postgres.BasicWaitStrategies()),
	)
	containerStartup = time.Since(started)
	return container, err
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
	containerUses.Add(1)
	dsn, err := sharedContainer.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatalf("read container connection string: %v", err)
	}
	return config.Config{Instance: harnessSchema, Database: config.Database{URL: dsn, Schema: harnessSchema}}
}

func restoreToSnapshot(t *testing.T) {
	t.Helper()
	skipIfShort(t)
	containerUses.Add(1)
	if err := sharedContainer.Restore(t.Context()); err != nil {
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
