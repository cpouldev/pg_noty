//go:build integration

package reconcile

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	harnessImage     = "postgres:17.10-alpine"
	harnessDatabase  = "notyreconcile"
	harnessUser      = "noty"
	harnessPassword  = "noty-harness-secret"
	harnessSchema    = "noty"
	harnessSQLDriver = "pgx"
)

var (
	sharedContainer   *postgres.PostgresContainer
	containersStarted atomic.Int64
)

func TestMain(m *testing.M) { os.Exit(runReconcileHarness(m)) }

func runReconcileHarness(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}
	ctx := context.Background()
	container, err := startReconcileContainer(ctx)
	if container != nil {
		defer container.Terminate(ctx)
	}
	if err != nil {
		return reconcileHarnessRefusal("start container: %v", err)
	}
	sharedContainer = container
	if err := container.Snapshot(ctx); err != nil {
		return reconcileHarnessRefusal("snapshot database: %v", err)
	}
	code := m.Run()
	if code != 0 {
		return code
	}
	if containersStarted.Load() != 1 {
		return reconcileHarnessRefusal("started %d containers, want 1", containersStarted.Load())
	}
	return 0
}

func startReconcileContainer(ctx context.Context) (*postgres.PostgresContainer, error) {
	containersStarted.Add(1)
	return postgres.Run(ctx, harnessImage,
		postgres.WithDatabase(harnessDatabase), postgres.WithUsername(harnessUser),
		postgres.WithPassword(harnessPassword), postgres.WithSQLDriver(harnessSQLDriver),
		postgres.BasicWaitStrategies())
}

// reportToHarnessStderr is the internal/source/testmain_integration_test.go:reportToHarnessStderr
// twin. The difference is the package under test and nothing else.
func reportToHarnessStderr(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "reconcile harness: "+format+"\n", arguments...)
}

func reconcileHarnessRefusal(format string, arguments ...any) int {
	reportToHarnessStderr(format, arguments...)
	return 1
}
