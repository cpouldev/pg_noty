//go:build integration

package reconcile

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Concurrent applies and the run-lock expiry half, both at the Apply boundary. The run-lock bound is
// an input on both sides -- above the holder's runtime in the acquisition case, below it in the
// expiry case -- so the two outcomes are produced by the input rather than by whichever process
// happened to be quicker.
const (
	aboveTheHoldersRuntime = 30 * time.Second
	belowTheHoldersRuntime = 50 * time.Millisecond
	contentionWatchdog     = 20 * time.Second
)

type applyOutcome struct {
	result ApplyResult
	err    error
	ddl    []string
}

// tracingPool opens a second session; tracedDatabase would restore the snapshot and kill the other run.
func tracingPool(t *testing.T) (*pgxpool.Pool, *recordedStatements) {
	t.Helper()
	poolConfig, err := pgxpool.ParseConfig(harnessConfig(t).Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	recorded := &recordedStatements{}
	poolConfig.ConnConfig.Tracer = recorded
	pool, err := pgxpool.NewWithConfig(t.Context(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, recorded
}

// oneListenerConfig is the bootstrapped database and one-listener configuration both suites apply.
func oneListenerConfig(t *testing.T, pool *pgxpool.Pool, table string) config.Config {
	t.Helper()
	prepareOwnershipDatabase(t, pool)
	mustExecOn(t, pool, "CREATE TABLE "+mustQualifiedTarget(t, table)+" (id bigint PRIMARY KEY)")
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{matrixListener(table, "insert")}
	return cfg
}

// holdTargetTable pins an apply inside its transaction: CREATE TRIGGER takes SHARE ROW EXCLUSIVE on the
// target, which this conflicts with, so the run cannot pass that statement until it is let go. The
// returned release is idempotent and also a cleanup: a caller that fails first would leave the run
// blocked, and pgxpool.Close waits for it, so the package would hang instead of reporting the failure.
func holdTargetTable(t *testing.T, pool *pgxpool.Pool, table string) func() {
	t.Helper()
	blocker := takeRunLockConnection(t, pool)
	mustExecOn(t, blocker, "BEGIN")
	mustExecOn(t, blocker, "LOCK TABLE "+mustQualifiedTarget(t, table)+" IN ACCESS EXCLUSIVE MODE")
	release := sync.OnceFunc(
		func() {
			blocker.Exec(context.Background(), "ROLLBACK")
			blocker.Release()
		},
	)
	t.Cleanup(release)
	return release
}

// applyOn runs one reconciler on its own pool and reports what it did, measured twice where a trace is
// supplied: the run's own statement count and the DDL its session actually put on the wire.
func applyOn(
	ctx context.Context,
	pool *pgxpool.Pool,
	trace *recordedStatements,
	cfg config.Config,
	opts Options,
) chan applyOutcome {
	done := make(chan applyOutcome, 1)
	go func() {
		result, err := Apply(ctx, pool, cfg, Approval{Approved: true}, opts)
		outcome := applyOutcome{result: result, err: err}
		if trace != nil {
			outcome.ddl = ddlStatements(trace.values())
		}
		done <- outcome
	}()
	return done
}

// waitUntilRunLockHeld reads the holder through the production reader, so observing cannot contend.
func waitUntilRunLockHeld(t *testing.T, on *pgxpool.Conn, key int64) string {
	t.Helper()
	for deadline := time.Now().Add(contentionWatchdog); time.Now().Before(deadline); {
		holder, err := runLockHolder(t.Context(), on, key)
		if err != nil {
			t.Fatal(err)
		}
		if holder != "could not be identified" {
			return holder
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no reconciler took the run lock, so the two applies never contended")
	return ""
}

// settleUntilRunLockFree answers how many further observations the key took to come free, and frees it.
func settleUntilRunLockFree(t *testing.T, on *pgxpool.Conn, key int64) int {
	t.Helper()
	for attempt := range int(contentionWatchdog / (10 * time.Millisecond)) {
		var locked bool
		if err := on.QueryRow(t.Context(), "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil {
			t.Fatal(err)
		}
		if locked {
			unlockRunLockKey(t, on, key)
			return attempt
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the run lock was never given back, so this path leaked it")
	return 0
}

// waitUntilBlocked returns once backend is waiting on a lock, through the run's own blocker reader.
func waitUntilBlocked(t *testing.T, on *pgxpool.Conn, backend int32) {
	t.Helper()
	for deadline := time.Now().Add(contentionWatchdog); time.Now().Before(deadline); {
		holder, err := blockingBackend(t.Context(), on, backend)
		if err != nil {
			t.Fatal(err)
		}
		if holder != "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf(
		"backend %d never blocked on the conflicting table lock, so a cancellation now would arrive between statements",
		backend,
	)
}

func TestTwoConcurrentAppliesProduceOneWinnerAndOneNoOp(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg := oneListenerConfig(t, pool, "concurrency_target")
	winnerPool, winnerTrace := tracingPool(t)
	loserPool, loserTrace := tracingPool(t)
	observer := takeRunLockConnection(t, pool)
	defer observer.Release()
	// The winner is pinned inside its transaction by a conflicting table lock, so it holds the run lock
	// -- and cannot finish -- while the loser starts. Its table-lock bound is well above the fixture's
	// runtime, so the conflict delays this apply rather than refusing it.
	releaseBlocker := holdTargetTable(t, pool, "concurrency_target")
	winner := applyOn(t.Context(), winnerPool, winnerTrace, cfg, Options{LockTimeout: aboveTheHoldersRuntime})
	heldBy := waitUntilRunLockHeld(t, observer, schema.ReconcileLockKey(harnessSchema, harnessSchema))

	// The loser supplies a run-lock bound ABOVE the winner's remaining runtime, which is the expiry
	// rule's other side: this case must end in an acquisition, not an expiry.
	loser := applyOn(t.Context(), loserPool, loserTrace, cfg, Options{RunLockWait: aboveTheHoldersRuntime})
	releaseBlocker()

	won, lost := <-winner, <-loser
	if won.err != nil || won.result.Verdict != VerdictClean || won.result.Statements == 0 || len(won.ddl) == 0 {
		t.Fatalf(
			"winner = %+v, %v; DDL=%q; want a clean run whose non-zero count makes the loser's zero a measurement",
			won.result,
			won.err,
			won.ddl,
		)
	}
	if lost.err != nil || lost.result.Verdict != VerdictClean || lost.result.Statements != 0 || len(lost.ddl) != 0 {
		t.Fatalf(
			"loser = %+v, %v; DDL=%q; want a measured zero against the winner's %d",
			lost.result,
			lost.err,
			lost.ddl,
			won.result.Statements,
		)
	}
	t.Logf(
		"run lock held by %s while the loser started; winner issued %d statements, loser %d",
		heldBy,
		won.result.Statements,
		lost.result.Statements,
	)
}

func TestAnApplyWhoseRunLockWaitExpiresIssuesNoDDLAndHoldsNoLock(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg := oneListenerConfig(t, pool, "expiry_target")
	key := schema.ReconcileLockKey(harnessSchema, harnessSchema)
	holder := takeRunLockConnection(t, pool)
	lockRunLockKey(t, holder, key)
	waiterPool, waiterTrace := tracingPool(t)
	// The bound is supplied BELOW the holder's remaining runtime: the holder is released only after
	// this apply has been answered, so the expiry is produced by the input rather than by timing.
	expired, err := Apply(
		t.Context(),
		waiterPool,
		cfg,
		Approval{Approved: true},
		Options{RunLockWait: belowTheHoldersRuntime},
	)
	waited := ddlStatements(waiterTrace.values())
	if err != nil || expired.Stopped != StopRunLockWaitExpired || expired.Statements != 0 || len(waited) != 0 {
		t.Fatalf(
			"expired apply = %+v, %v; DDL=%q; want its named wait-expired verdict and zero DDL",
			expired,
			err,
			waited,
		)
	}
	if len(expired.Plan.Refusals) != 1 || !strings.Contains(expired.Plan.Refusals[0].Message(), harnessSchema) ||
		!strings.Contains(expired.Plan.Refusals[0].Message(), backendIdentity(t, holder)) {
		t.Fatalf(
			"expiry refusals = %+v; want one naming the instance and the backend holding its lock",
			expired.Plan.Refusals,
		)
	}
	unlockRunLockKey(t, holder, key)
	holder.Release()

	// The same configuration applied once the lock is free, so the zero above measures a refused run
	// rather than one with nothing to do (count-the-population-a-vacuity-guard-guards.md).
	settled, err := Apply(t.Context(), waiterPool, cfg, Approval{Approved: true}, Options{})
	if err != nil || settled.Verdict != VerdictClean || settled.Statements == 0 {
		t.Fatalf("apply after the holder let go = %+v, %v; want the work the expired run declined", settled, err)
	}
}
