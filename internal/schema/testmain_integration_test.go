//go:build integration

package schema

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the harnessSQLDriver name for snapshot/restore
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// This file is the container harness Steps 8 to 16 inherit. Eleven of the seventeen steps run the
// tagged suite, so what it promises them is written down here rather than rediscovered per step:
//
//   - ONE container serves the whole package. Nothing but startSharedContainer may call
//     postgres.Run, and containersStarted refuses the run if anything did.
//   - A test gets a clean database from freshDatabase(t), which restores the snapshot and *then*
//     opens the pool. That order is not a style: Restore drops and recreates the database, so a
//     pool opened before it is dead afterwards, and a test holding one across a restore is a bug.
//   - No test may assume state left by another. Restore takes the database back to the empty
//     snapshot, which keeps Step 11's blocked-DEFAULT case from poisoning every case after it.
//   - Every container-backed test calls skipIfShort first
//     (TestEveryContainerBackedTestCallsTheShortCircuit).
//
// The container's address, name and daemon are all resolved by testcontainers-go from the
// environment, so this file names none of them: three of these processes run concurrently at slots
// S6 and S8, and DOCKER_HOST is how a remote or rootless daemon serves the tier unchanged.

var (
	// sharedContainer is the one container. Steps 8 to 16 reach it through the helpers below rather
	// than directly, so a later step cannot restore it without counting as a user of it.
	sharedContainer *postgres.PostgresContainer
	// containersStarted counts calls to postgres.Run, which is the only assertion that tells one
	// container from N: a single call written in TestMain is equally consistent with a helper that
	// starts one per test.
	containersStarted atomic.Int64
	// containerUses counts the tests that reached the container, which harnessVerdict reads.
	containerUses atomic.Int64
	// containerStartup is what a container costs, measured rather than assumed. It is what
	// TestRestoringIsCheaperThanStartingAnotherContainer compares a restore against, so that claim
	// rests on two measurements from one machine rather than on a magic constant.
	containerStartup time.Duration
)

func TestMain(m *testing.M) {
	os.Exit(runAgainstOneContainer(m))
}

// runAgainstOneContainer owns the container's whole life, and returns rather than exiting so that
// its deferred termination runs.
func runAgainstOneContainer(m *testing.M) int {
	// Parsed here because testing.Short reads a flag, and the tier is skipped before a container is
	// started rather than after.
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}

	ctx := context.Background()
	container, err := startSharedContainer(ctx)
	if container != nil {
		defer terminateSharedContainer(ctx, container)
	}
	if err != nil {
		return refuse("start the %s container: %v", harnessImage, err)
	}
	sharedContainer = container

	if err := container.Snapshot(ctx); err != nil {
		return refuse("snapshot %s: %v", harnessDatabase, err)
	}
	return harnessVerdict(m.Run())
}

// startSharedContainer is this package's only call to postgres.Run, and it counts before it calls
// so that a start which then fails is still counted as one.
func startSharedContainer(ctx context.Context) (*postgres.PostgresContainer, error) {
	containersStarted.Add(1)
	began := time.Now()

	container, err := postgres.Run(
		ctx, harnessImage,
		postgres.WithDatabase(harnessDatabase),
		postgres.WithUsername(harnessUser),
		postgres.WithPassword(harnessPassword),
		// Without this the module opens its snapshot connection under the "postgres" driver name,
		// which nothing registers, and every restore falls back to `docker exec psql`.
		postgres.WithSQLDriver(harnessSQLDriver),
		postgres.BasicWaitStrategies(),
	)
	containerStartup = time.Since(began)

	return container, err
}

func terminateSharedContainer(ctx context.Context, container *postgres.PostgresContainer) {
	if err := container.Terminate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "terminate the %s container: %v\n", harnessImage, err)
	}
}

// harnessVerdict fails a run the tests themselves reported as green but the harness did not.
// Neither condition can be seen from inside a test: a per-test container is only visible once every
// test has run, and a suite that reached no container is only visible then too.
//
// The second is asked of a whole run only. A green *filtered* run is not a claim about the tier --
// it is a claim about the tests named -- so `-run` naming none of the container-backed ones is a
// choice rather than the vacuity this guard is for.
func harnessVerdict(code int) int {
	switch {
	case code != 0:
		return code
	case containersStarted.Load() != 1:
		return refuse(
			"%d containers were started for this package, want exactly 1; a helper is "+
				"starting one per test and nine later steps inherit it", containersStarted.Load(),
		)
	case containerUses.Load() == 0 && !someTestsWereFilteredOut():
		return refuse(
			"a container was started and no test reached it, so the whole tagged tier " +
				"is green over nothing",
		)
	}
	return 0
}

// someTestsWereFilteredOut reports whether this run was asked for a subset of the package's tests.
func someTestsWereFilteredOut() bool {
	selecting := flag.Lookup("test.run")
	return selecting != nil && selecting.Value.String() != ""
}

// refuse reports why the harness failed the run and answers with the failing exit code, so a caller
// writes one statement rather than a print and a return that can disagree.
func refuse(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "harness: "+format+"\n", args...)
	return 1
}

// skipIfShort is the secondary guard skill Pattern 8 asks for. The build tag is the primary one;
// this is what lets `-short` skip the tier without editing tags.
func skipIfShort(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping the container-backed tier under -short")
	}
}

// harnessConfig is where the container is, as the configuration OpenPool reads. Later steps that
// need their own schema, instance or retention copy it and set what they need.
func harnessConfig(t *testing.T) config.Config {
	t.Helper()
	skipIfShort(t)
	containerUses.Add(1)

	dsn, err := sharedContainer.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatalf("read the container's connection string: %v", err)
	}
	return config.Config{
		Instance: harnessSchema,
		Database: config.Database{URL: dsn, Schema: harnessSchema},
	}
}

// restoreToSnapshot takes the database back to the state TestMain snapshotted, and answers with
// what that cost. Every pool open before it is dead after it, because the mechanism drops and
// recreates the database rather than truncating inside it.
func restoreToSnapshot(t *testing.T) time.Duration {
	t.Helper()
	skipIfShort(t)
	containerUses.Add(1)

	began := time.Now()
	if err := sharedContainer.Restore(t.Context()); err != nil {
		t.Fatalf("restore %s to its snapshot: %v", harnessDatabase, err)
	}
	return time.Since(began)
}

// freshDatabase is the shape the eight consuming steps call: an empty database, and a pool onto it
// that closes with the test. It is one helper rather than one per suite so that no step invents its
// own reset, and it opens through OpenPool so that every container-backed test exercises the
// production connection path (ADR-2).
func freshDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	restoreToSnapshot(t)

	pool, err := OpenPool(t.Context(), harnessConfig(t))
	if err != nil {
		t.Fatalf("open a pool onto the restored database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
