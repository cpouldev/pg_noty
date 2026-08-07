//go:build integration

package reconcile

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The cancellation exit path (f), driven at the lock's own level rather than through Apply's wiring.
//
// runlock_integration_test.go's TestCancelledRunContextFailsTheNaiveReleaseButNotTheSuppliedOne is
// this file's twin for a cancellation reaching an *idle* pinned connection, where the release is a
// pg_advisory_unlock a cancelled context refuses to send. The case here is the one an interrupted
// Apply actually meets: the cancellation arrives while a statement is executing, which closes the
// pinned connection, so no session is left to unlock on and the key is given back by ending that
// session instead. Both routes belong to the release, and a cancelled context refuses both -- which
// is what makes exit path (f) falsify the release context rather than describe it.

// aRunLockClosedMidStatement holds the run lock on a connection closed by a cancellation that arrived
// while its statement was executing, and answers the session that watches the key from outside.
func aRunLockClosedMidStatement(t *testing.T, table string) (*runLock, *pgxpool.Conn, context.Context) {
	t.Helper()
	pool := freshDatabase(t)
	target := mustQualifiedTarget(t, table)
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	observer := takeRunLockConnection(t, newRunLockPool(t, 1))
	t.Cleanup(observer.Release)
	holdTargetTable(t, pool, table) // let go by its own cleanup, after this test's assertions

	held, refusal, err := acquireRunLock(t.Context(), pool, harnessSchema, harnessSchema, time.Second)
	if err != nil || refusal != nil || held == nil {
		t.Fatalf("acquire the run lock this case cancels = (%v, %v, %v)", held, refusal, err)
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	t.Cleanup(cancel)
	// The conflicting table lock is what makes the timing an input rather than a hope: the statement
	// is measured to be executing and waiting before the cancellation is delivered.
	executing := make(chan error, 1)
	go func() {
		tx, err := held.on.Begin(runCtx)
		if err != nil {
			executing <- err
			return
		}
		_, err = tx.Exec(runCtx, "LOCK TABLE "+target+" IN ACCESS EXCLUSIVE MODE")
		executing <- err
	}()
	waitUntilBlocked(t, observer, held.backend)
	cancel()

	if err := <-executing; !errors.Is(err, context.Canceled) {
		t.Fatalf("the pinned statement ended with %v, want the cancellation this case is named for", err)
	}
	// The precondition this fixture is named for, asserted rather than assumed: generated timing that
	// delivered the cancellation between statements would leave the connection open and quietly turn
	// every case below into the idle-connection one its twin already covers.
	if !held.on.Conn().IsClosed() {
		t.Fatal("the pinned connection is still open, so the cancellation did not arrive mid-statement " +
			"and this is not the case these tests are about")
	}
	return held, observer, runCtx
}

// TestAMidStatementCancellationFreesTheKeyOnlyUnderAnUncancelledContext is the falsification exit
// path (f) rests on: one closed session, released under two contexts that differ in nothing else.
func TestAMidStatementCancellationFreesTheKeyOnlyUnderAnUncancelledContext(t *testing.T) {
	skipIfShort(t)
	t.Run("the run's own cancelled context never reaches the session", func(t *testing.T) {
		held, observer, runCtx := aRunLockClosedMidStatement(t, "cancelled_release_naive")

		// The refusal is asserted where it happens -- at the acquire, before any statement is sent --
		// rather than as "the key is still held", and that is a measurement rather than a preference.
		// Once this release has refused, the only thing still holding the key is an abandoned backend
		// pgx is concurrently destroying: measured over 15 runs it was held on the first probe every
		// time, but for only 0.93ms to 2.5ms longer, against a 0.7ms probe. An assertion with a 1ms
		// margin is the flaky case this step's Risks forbid, so what is asserted here is the half that
		// cannot race, and the key's own state is asserted in the sibling case below, where the repair
		// makes it deterministic.
		err := held.release(runCtx)
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "reach the session") {
			t.Fatalf("release under the run's cancelled context = %v, want that cancellation to refuse "+
				"the recovery at its acquire, before it can end anything", err)
		}
		// Teardown, and the one lock-level fact this direction can state without racing: the session
		// the refused release walked away from was still there to be ended.
		if err := held.endTheSessionStillHoldingTheKey(context.Background()); err != nil {
			t.Fatalf("end the session the refused release left holding the key: %v", err)
		}
		assertRunLockFree(t, observer, held.key)
	})
	t.Run("a context the cancellation did not reach frees it before returning", func(t *testing.T) {
		held, observer, runCtx := aRunLockClosedMidStatement(t, "cancelled_release_supplied")
		if err := held.release(context.WithoutCancel(runCtx)); err != nil {
			t.Fatalf("release under a context the cancellation did not reach: %v", err)
		}
		// Taken and given back on the observer, with no further observation in between: the key is
		// free at the moment release returned, not eventually.
		assertRunLockFree(t, observer, held.key)
	})
}

// TestACancelledStatementLeavesThePinnedConnectionClosed pins the pgx behaviour the release branches
// on. Both clauses matter: the flag release reads, and what that flag is taken to mean. If a later
// pgx hands the connection back open, the closed-session branch is unreachable and the recovery must
// be re-derived; if it reports closed and still serves statements, the branch is being entered for a
// session that could have unlocked.
func TestACancelledStatementLeavesThePinnedConnectionClosed(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	pinned := takeRunLockConnection(t, pool)
	defer pinned.Release()
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	time.AfterFunc(50*time.Millisecond, cancel)

	if _, err := pinned.Exec(ctx, "SELECT pg_sleep(30)"); !errors.Is(err, context.Canceled) {
		t.Fatalf("statement cancelled mid-flight = %v, want context.Canceled", err)
	}
	if !pinned.Conn().IsClosed() {
		t.Fatal("the connection reads as open after a cancelled statement, so release's closed-session " +
			"branch is unreachable and its recovery must be re-derived")
	}
	if _, err := pinned.Exec(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("a connection reading as closed served a statement, so IsClosed no longer means the " +
			"session cannot be asked to unlock")
	}
}
