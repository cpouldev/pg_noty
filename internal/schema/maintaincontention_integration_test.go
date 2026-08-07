//go:build integration

package schema

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 40's *create* part and criterion 33's distinction between a pass that did
// nothing and a pass that failed.
//
// D3 splits criterion 40 in three: the mechanism -- that a bounded statement gives up and leaves
// the schema unchanged -- is Step 8's, the create is here, and the drop is Step 14's. None is
// implied by another, and no owning step cites another's row: this file reaches the create branch
// with a conflicting lock of its own, over the event log the pass actually maintains. What it takes
// from Step 8 is two durations -- conflictingBound and corroborationMargin -- and a number is not
// evidence; the fixture, the pass and every assertion below are this step's.

// holdTheEventLogLocked takes ACCESS EXCLUSIVE on the event log alone and answers with the release.
//
// ONLY is what makes this the *create* fixture rather than a fixture for whatever the pass does
// first. A lock taken without it descends to every partition, so the pass's observation -- which
// counts the rows in the DEFAULT partition -- would abandon before a create was ever attempted, and
// the branch this case is named for would go unreached. A create needs the parent, which is exactly
// what this holds (M5).
func holdTheEventLogLocked(t *testing.T, pool *pgxpool.Pool) func() {
	t.Helper()

	tx, err := acquiredConn(t, pool).Begin(t.Context())
	if err != nil {
		t.Fatalf("begin the transaction holding the conflicting lock: %v", err)
	}
	mustExecOn(t, tx, "LOCK TABLE ONLY "+mustQualify(t, harnessSchema, TableEvents)+
		" IN ACCESS EXCLUSIVE MODE")

	// Registered as well as returned, so a case that never releases early still hands the next one a
	// usable database. t.Context() is cancelled just before cleanups run and a rollback needs one
	// that is not; rolling back twice answers pgx.ErrTxClosed and changes nothing.
	release := func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }
	t.Cleanup(release)
	return release
}

// theBackendsQueuedOn counts the backends waiting for a lock on one relation. It is what lets a
// case that has to reach a genuine race wait until its replicas are queued, rather than sleeping
// and hoping.
const theBackendsQueuedOn = `SELECT count(*) FROM pg_locks
WHERE relation = to_regclass($1)::oid AND NOT granted`

// waitUntilBackendsAreQueuedOn blocks until that many backends are queued on one relation, and
// fails rather than returning early: a case that carried on with fewer would be asserting about a
// race that never happened.
func waitUntilBackendsAreQueuedOn(t *testing.T, pool *pgxpool.Pool, relation string, queued int) {
	t.Helper()

	for deadline := time.Now().Add(corroborationMargin); time.Now().Before(deadline); {
		var waiting int
		if err := pool.QueryRow(t.Context(), theBackendsQueuedOn, relation).Scan(&waiting); err != nil {
			t.Fatalf("count the backends queued on %s: %v", relation, err)
		}
		if waiting >= queued {
			return
		}
		time.Sleep(theQueuePollInterval)
	}
	t.Fatalf("%d backends were not queued on %s within %s, so the contention this case is named for "+
		"was never set up", queued, relation, corroborationMargin)
}

// theQueuePollInterval is how often the wait above looks. Short enough that the lock is released
// promptly once every replica is queued, and long enough not to be a busy loop.
const theQueuePollInterval = 10 * time.Millisecond

// TestACreateMeetingAConflictingLockAbandonsAndLeavesTheSchemaUnchanged is criterion 40's create
// half. The classification is the evidence and the clock only corroborates it: a wall-clock
// assertion read as primary evidence is flaky on a loaded machine, and a flake here reads as an
// infrastructure problem rather than as the wrong assertion it is.
func TestACreateMeetingAConflictingLockAbandonsAndLeavesTheSchemaUnchanged(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aMaintainedEventLog(t)
	before := partitionInventory(t, pool)
	clock := theClockOf(t, pool)
	release := holdTheEventLogLocked(t, pool)

	began := time.Now()
	report := onePass(t, pool, cfg, Options{LockTimeout: conflictingBound})
	elapsed := time.Since(began)
	release()

	wanted := theHorizonBetween(t, clock, theClockOf(t, pool))
	if !errors.Is(report.refused, ErrLockTimeout) {
		t.Fatalf("the pass answered %v, want %v: a conflicting ACCESS EXCLUSIVE lock on %s was held "+
			"for the whole attempt", report.refused, ErrLockTimeout, TableEvents)
	}
	assertLockTimeoutObserved(t, report, len(wanted))

	// One bound per range, because each range is attempted on its own and each meets the same lock.
	if bound := time.Duration(len(wanted))*conflictingBound + corroborationMargin; elapsed > bound {
		t.Errorf("the pass gave up after %s, more than %d bounds of %s plus %s of slack; the bound "+
			"may not have reached the server at all",
			elapsed, len(wanted), conflictingBound, corroborationMargin)
	}
	// Unchanged, so a later pass simply retries. Read after the release, because the bound-reading
	// half of an inventory would itself wait on the lock the episode is about.
	assertInventoryUnchanged(t, before, partitionInventory(t, pool), "a pass that timed out")
}

// assertLockTimeoutObserved is what the caller and the operator see. The two counters are asserted
// together and in both directions: the general failure count moves, and the DEFAULT-partition count
// does not -- without the second clause a pass that counted every refusal as a blocked range would
// pass, and an operator would be sent to drain rows over a lock that had already been released.
func assertLockTimeoutObserved(t *testing.T, report passReport, ranges int) {
	t.Helper()

	if errors.Is(report.refused, ErrDefaultBlocked) {
		t.Errorf("the timeout also matches %v, so the two conditions are one error with two "+
			"messages", ErrDefaultBlocked)
	}
	if want := (statsReading{failures: int64(ranges)}); report.stats != want {
		t.Errorf("the pass recorded %+v, want %+v: one failure per range it could not create, and "+
			"nothing on the counter whose remedy is a drain", report.stats, want)
	}
	if want := (Result{Outcome: OutcomeFailed}); report.settled != want {
		t.Errorf("the pass reported %+v, want %+v", report.settled, want)
	}

	refusals := refusalsLogged(report)
	if len(refusals) != ranges {
		t.Errorf("the pass logged %d refusals for %d refused ranges; each range is attempted, "+
			"counted and logged on its own", len(refusals), ranges)
	}
	for _, logged := range refusals {
		if strings.Contains(logged.attrs[logRemedy], repairCommand) {
			t.Errorf("range %s timed out and is logged with the remedy %s; that command moves rows, "+
				"and this range needs only the next pass", logged.attrs[logRange], logged.attrs[logRemedy])
		}
	}
}

// TestAPassThatFoundNothingToDoIsNotAPassThatFailed is criterion 33's inequality against the
// server, over two passes that both changed nothing.
//
// Without it the two collapse: a maintenance loop that has been failing for a week reports the same
// Result as one with nothing to do, and reads as healthy for as long as nobody looks. The Results
// are compared as values, and the levels the two were logged at are compared as well, because a
// caller reads the first and an alerting rule reads the second.
func TestAPassThatFoundNothingToDoIsNotAPassThatFailed(t *testing.T) {
	skipIfShort(t)

	pool, cfg, wanted := aConvergedEventLog(t)
	quiet := onePass(t, pool, cfg, Options{})
	stillTheSameHorizon(t, pool, cfg, wanted)

	// One range removed by hand, so the second pass has work to do -- and a lock it cannot take.
	mustExecOn(t, pool, "DROP TABLE "+mustQualify(t, harnessSchema, wanted[0].Name))
	release := holdTheEventLogLocked(t, pool)
	failed := onePass(t, pool, cfg, Options{LockTimeout: conflictingBound})
	release()

	if quiet.settled == failed.settled {
		t.Fatalf("both passes reported %+v, so a caller cannot tell one that found nothing to do "+
			"from one that could not do it", quiet.settled)
	}
	if want := (Result{Outcome: OutcomeNothingNeeded}); quiet.settled != want || quiet.refused != nil {
		t.Errorf("the quiet pass reported %+v and %v, want %+v and no error",
			quiet.settled, quiet.refused, want)
	}
	assertReportedAboveRoutineOperation(t, quiet, failed)
}

// assertReportedAboveRoutineOperation is criterion 33's third clause: the failed pass is logged
// where an operator's alerting can key on it, and the quiet one is not. Both directions, because a
// package that logged everything at the same level would satisfy either clause alone.
func assertReportedAboveRoutineOperation(t *testing.T, quiet, failed passReport) {
	t.Helper()

	if raised := quiet.loggedAt(slog.LevelError); len(raised) != 0 {
		t.Errorf("the pass that found nothing to do wrote %d records at %s, so routine operation is "+
			"indistinguishable from failure", len(raised), slog.LevelError)
	}
	if len(quiet.logged) == 0 {
		t.Error("the pass that found nothing to do wrote no record at all, so there is nothing for " +
			"the level of a failure to be distinguishable from")
	}
	if raised := failed.loggedAt(slog.LevelError); len(raised) == 0 {
		t.Errorf("the failed pass wrote %d records and none at %s", len(failed.logged), slog.LevelError)
	}
}
