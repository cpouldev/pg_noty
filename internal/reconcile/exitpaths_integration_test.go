//go:build integration

package reconcile

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// internal/schema measured the failure this file exists for: "a session lock unlocked from a
// connection other than the one that took it never unlocks at all, and that connection returns to the
// pool still holding it." Each path is therefore followed by the two observations that are what "one
// pinned connection" looks like from outside: the key free again from a session that is not the
// holder, and the pool back to the connection count it began with. The type-level half -- that a
// release cannot be spelled without the connection that took the lock -- is
// TestRunLockReleaseRequiresTheHandleCarryingThePinnedConnection in runlock_test.go.
type exitPath struct {
	letter, name string
	exit         func(t *testing.T, pool *pgxpool.Pool)
}

var theSevenExitPaths = []exitPath{
	{letter: "a", name: "success", exit: exitBySuccess},
	{letter: "b", name: "ownership refusal", exit: exitByOwnershipRefusal},
	{letter: "c", name: "validation failure", exit: exitByValidationFailure},
	{letter: "d", name: "table-lock timeout", exit: exitByTableLockTimeout},
	{letter: "e", name: "injected failure mid-run", exit: exitByInjectedFailure},
	{letter: "f", name: "cancelled or interrupted", exit: exitByCancellation},
	{letter: "g", name: "run-lock wait expired before acquisition", exit: exitByRunLockWaitExpiry},
}

// TestTheSevenExitPathsAreADeclaredSizePinnedSet makes an eighth path fail here rather than be the one
// nobody checked (assert-a-set-wide-invariant-over-the-set.md).
func TestTheSevenExitPathsAreADeclaredSizePinnedSet(t *testing.T) {
	skipIfShort(t)
	if len(theSevenExitPaths) != 7 {
		t.Fatalf("%d exit paths are declared, want the seven this file enumerates", len(theSevenExitPaths))
	}
	for index, path := range theSevenExitPaths {
		if want := string(rune('a' + index)); path.letter != want {
			t.Errorf("exit path %d is lettered %q, want %q; the set is ordered a to g", index, path.letter, want)
		}
	}
}

func TestTheRunLockIsFreeAfterEveryOneOfTheSevenExitPaths(t *testing.T) {
	skipIfShort(t)
	key := schema.ReconcileLockKey(harnessSchema, harnessSchema)
	for _, path := range theSevenExitPaths {
		t.Run(
			path.letter+"_"+strings.ReplaceAll(path.name, " ", "_"), func(t *testing.T) {
				pool := freshDatabase(t)
				observer := takeRunLockConnection(t, newRunLockPool(t, 1))
				defer observer.Release()
				before := pool.Stat().AcquiredConns()

				path.exit(t, pool)

				if settled := settleUntilRunLockFree(t, observer, key); settled != 0 {
					t.Errorf(
						"path (%s) needed %d further observations before the key came free; every path "+
							"gives it back before Apply returns", path.letter, settled,
					)
				}
				if got := pool.Stat().AcquiredConns(); got != before {
					t.Fatalf(
						"path (%s) left %d acquired pool connections, want the %d it began with; a "+
							"connection still pinned is the leak internal/schema measured", path.letter, got, before,
					)
				}
			},
		)
	}
}

func exitBySuccess(t *testing.T, pool *pgxpool.Pool) {
	result, err := Apply(
		t.Context(),
		pool,
		oneListenerConfig(t, pool, "exit_success"),
		Approval{Approved: true},
		Options{},
	)
	if err != nil || result.Verdict != VerdictClean || result.Statements == 0 {
		t.Fatalf("(a) success = %+v, %v; want a changed clean run", result, err)
	}
}

func exitByOwnershipRefusal(t *testing.T, pool *pgxpool.Pool) {
	cfg, listener, target := appliedTargetFixture(t, pool, "exit_ownership", "insert")
	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener.Name)
	if err != nil || len(reading.Triggers) != 1 {
		t.Fatalf("registry = %+v, %v", reading, err)
	}
	mustExecOn(t, pool, "COMMENT ON TRIGGER \""+reading.Triggers[0].TriggerName+"\" ON "+target+" IS NULL")
	cfg.Listeners = nil
	result, err := Apply(t.Context(), pool, cfg, Approval{Approved: true, DestructionPermitted: true}, Options{})
	if err != nil || result.Stopped != StopOwnership {
		t.Fatalf("(b) ownership refusal = %+v, %v; want the run stopped on ownership", result, err)
	}
}

func exitByValidationFailure(t *testing.T, pool *pgxpool.Pool) {
	cfg := oneListenerConfig(t, pool, "exit_validation")
	cfg.Listeners[0].Trigger.Operations[0].When = "NEW.missing > 0"
	result, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{})
	if err != nil || result.Stopped != StopValidation {
		t.Fatalf("(c) validation failure = %+v, %v; want the run stopped on validation", result, err)
	}
}

func exitByTableLockTimeout(t *testing.T, pool *pgxpool.Pool) {
	cfg := oneListenerConfig(t, pool, "exit_timeout")
	defer holdTargetTable(t, pool, "exit_timeout")()
	_, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{LockTimeout: 200 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "exit_timeout") {
		t.Fatalf("(d) table-lock timeout = %v; want a refusal naming the table it could not lock", err)
	}
}

// exitByInjectedFailure fails the run at its registry write, the last thing an apply does, so its DDL
// has already been issued when the planted constraint refuses this listener's row.
func exitByInjectedFailure(t *testing.T, pool *pgxpool.Pool) {
	cfg := oneListenerConfig(t, pool, "exit_injected")
	listeners, err := qualifiedRegistryTable(harnessSchema, schema.TableListeners)
	if err != nil {
		t.Fatal(err)
	}
	mustExecOn(
		t, pool, "ALTER TABLE "+listeners+" ADD CONSTRAINT exit_injected_refusal CHECK (name <> "+
			harnessLiteral(cfg.Listeners[0].Name)+")",
	)
	if _, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err == nil {
		t.Fatal("(e) injected failure: the apply succeeded despite the constraint refusing its registry row")
	}
}

// exitByCancellation is the path internal/schema's failure mode is reached on: on every other one
// the run's context is still live when the lock is given back. waitUntilBlocked measures that
// CREATE TRIGGER is executing and waiting before the cancel, so the timing is deterministic rather
// than hopeful.
//
// It falsifies the release context as well. A cancellation arriving mid-statement closes the pinned
// connection, so nothing can be unlocked on it and the key is given back by ending that session
// instead -- which needs a context the cancellation did not reach. An implementation passing the run's
// own cancelled context cannot even reach the session, leaves the key held, and fails the settled
// count above -- measured over ten runs rather than argued, because such a mutant's key does come free
// a millisecond or two later on its own. Both directions are driven without Apply's wiring by
// TestAMidStatementCancellationFreesTheKeyOnlyUnderAnUncancelledContext
// (cancelledrelease_integration_test.go), whose idle-connection twin is
// TestCancelledRunContextFailsTheNaiveReleaseButNotTheSuppliedOne.
func exitByCancellation(t *testing.T, pool *pgxpool.Pool) {
	cfg := oneListenerConfig(t, pool, "exit_cancelled")
	defer holdTargetTable(t, pool, "exit_cancelled")()
	observer := takeRunLockConnection(t, pool)
	runCtx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	defer cancel()

	done := applyOn(runCtx, pool, nil, cfg, Options{LockTimeout: aboveTheHoldersRuntime})
	backend := waitUntilRunLockHeld(t, observer, schema.ReconcileLockKey(harnessSchema, harnessSchema))
	waitUntilBlocked(t, observer, int32From(t, backend))
	observer.Release()
	cancel()

	if got := <-done; got.err == nil {
		t.Fatalf("(f) cancellation = %+v; want the run to end on its cancelled context", got.result)
	}
	if runCtx.Err() == nil {
		t.Fatal("(f) cancellation: the run's context is live, so it was never the interrupted run this case names")
	}
}

func exitByRunLockWaitExpiry(t *testing.T, pool *pgxpool.Pool) {
	cfg := oneListenerConfig(t, pool, "exit_expiry")
	key := schema.ReconcileLockKey(harnessSchema, harnessSchema)
	holder := takeRunLockConnection(t, pool)
	lockRunLockKey(t, holder, key)
	result, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{RunLockWait: belowTheHoldersRuntime})
	if err != nil || result.Stopped != StopRunLockWaitExpired || result.Statements != 0 {
		t.Fatalf("(g) run-lock wait expiry = %+v, %v; want its named verdict and zero DDL", result, err)
	}
	// This fixture's holder is the only thing still on the key, so releasing it here is what lets the
	// caller's non-holding observer answer for the run rather than for the fixture.
	unlockRunLockKey(t, holder, key)
	holder.Release()
}
