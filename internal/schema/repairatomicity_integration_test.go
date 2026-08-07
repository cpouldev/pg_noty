//go:build integration

package schema

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is SC-4: one transaction, bounded by ddl.go, and a failure that leaves the database
// exactly as it was.
//
// The failure is induced by a real conflicting lock rather than by a contrived timeout, and the
// lock is chosen so that it lands *mid-drain* -- after the rows have left the DEFAULT partition and
// before they are put back, which is the state "one transaction" exists to make unreachable. That
// it really lands there is measured rather than asserted in prose, by
// TestTheConflictingLockLetsTheMoveOutRunAndStopsTheCreate.

// queueRows is how many rows the delivery queue holds, which is the second half of the unchanged
// claim: an inventory alone would pass a drain that moved rows out and failed to move them back,
// since no relation changed.
func queueRows(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()

	var held int
	statement := countRowsPrefix + mustQualify(t, harnessSchema, TableEventQueue)
	if err := pool.QueryRow(t.Context(), statement).Scan(&held); err != nil {
		t.Fatalf("count the rows in %s: %v", TableEventQueue, err)
	}
	return held
}

// countingBeginner is the handle a drain is given so the transactions it opens can be counted.
// RepairDefaultPartition takes ddl.go's beginner rather than a pool precisely so a caller may hand
// it either, and that is what lets "one transaction" be asserted at the seam instead of inferred.
type countingBeginner struct {
	on     beginner
	begins int
}

func (counting *countingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	counting.begins++
	return counting.on.Begin(ctx)
}

// TestTheWholeDrainRunsInOneTransaction is SC-4's first clause. The move out, the create and the
// move back have to fail together or not at all: across two transactions, a failure of the second
// leaves the customer's rows in a holding table nobody owns, and no rollback recovers that.
//
// The moved count is checked first as this case's own precondition -- a drain that moved nothing
// would open one transaction too, and the case named for atomicity would be asserting about a
// no-op.
func TestTheWholeDrainRunsInOneTransaction(t *testing.T) {
	skipIfShort(t)

	blocked := aBlockedEventLog(t)
	defer restoreToSnapshot(t)
	counting := &countingBeginner{on: blocked.pool}

	done, err := RepairDefaultPartition(t.Context(), counting, blocked.cfg, Options{}, blocked.blocked)
	if err != nil {
		t.Fatalf("drain %s: %v", extentOf(blocked.blocked), err)
	}
	if planted := int64(len(blocked.drained)); done.Moved != planted {
		t.Fatalf("the drain moved %d rows and %d were planted, so this case has not exercised a drain "+
			"that moved anything", done.Moved, planted)
	}
	if counting.begins != 1 {
		t.Errorf("the drain opened %d transactions, want exactly 1", counting.begins)
	}
}

// TestADrainMeetingAConflictingLockLeavesTheSchemaAndTheRowsExactlyAsTheyWere is SC-4's second half.
// The classification is the evidence and the clock only corroborates it, for the reason Step 8
// recorded: a wall-clock assertion read as primary evidence is flaky on a loaded machine, and a
// flake here reads as an infrastructure problem rather than as the wrong assertion it is.
func TestADrainMeetingAConflictingLockLeavesTheSchemaAndTheRowsExactlyAsTheyWere(t *testing.T) {
	skipIfShort(t)

	blocked := aBlockedEventLog(t)
	defer restoreToSnapshot(t)

	inventory, events := partitionInventory(t, blocked.pool), eventInventory(t, blocked.pool)
	inDefault, queued := rowsInDefault(t, blocked.pool), queueRows(t, blocked.pool)

	release := holdTheEventLogLocked(t, blocked.pool)
	began := time.Now()
	done, err := RepairDefaultPartition(t.Context(), blocked.pool, blocked.cfg,
		Options{LockTimeout: conflictingBound}, blocked.blocked)
	elapsed := time.Since(began)
	release()

	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("the drain answered %v, want %v: a conflicting ACCESS EXCLUSIVE lock on %s was held "+
			"for the whole attempt", err, ErrLockTimeout, TableEvents)
	}
	if done != (RepairResult{}) {
		t.Errorf("the drain reported %+v alongside a refusal, and a caller reading a moved count off "+
			"a drain that moved nothing would believe the range had been cleared", done)
	}
	if bound := conflictingBound + corroborationMargin; elapsed > bound {
		t.Errorf("the drain gave up after %s, more than the %s bound plus %s of slack; the bound may "+
			"not have reached the server at all", elapsed, conflictingBound, corroborationMargin)
	}
	assertNothingHappened(t, blocked, inventory, events, inDefault, queued)
}

// assertNothingHappened is the unchanged claim in all four of its parts: the schema, every event
// with the partition holding it, the DEFAULT partition's own row count, and the queue's.
//
// Together with the precondition below, this *is* the one-transaction assertion rather than a claim
// beside it. The lock this drain met provably lets the delete through, so the rows did leave the
// DEFAULT partition; that they are all back afterwards, with no holding table and no partition, can
// only be a rollback of the same transaction the create was refused in. An inventory alone would
// pass a drain that moved rows out across two transactions and never moved them back, which is why
// the row counts are here as well.
func assertNothingHappened(t *testing.T, blocked blockedLog, inventory []string,
	events map[int64]eventRow, inDefault int64, queued int) {
	t.Helper()

	assertInventoryUnchanged(t, inventory, partitionInventory(t, blocked.pool), "a drain that timed out")
	if held := rowsInDefault(t, blocked.pool); held != inDefault {
		t.Errorf("the DEFAULT partition holds %d rows after the refused drain and held %d before it",
			held, inDefault)
	}
	if held := queueRows(t, blocked.pool); held != queued {
		t.Errorf("%s holds %d rows after the refused drain and held %d before it",
			TableEventQueue, held, queued)
	}
	assertLandedIn(t, blocked.pool, events, partitionsOf(events))

	// A holding table outliving its transaction is the failure mode that makes a drain across
	// several transactions unusable: the customer's rows would be sitting in a table nobody owns.
	for _, relation := range []string{TableEvents, TableEventQueue} {
		if identity, rendered := identityOf(t, blocked.pool, harnessSchema,
			holdingName(relation, blocked.blocked)); identity != 0 {
			t.Errorf("%s survived the refused drain holding rows nobody owns", rendered)
		}
	}
}

// partitionsOf is the partition each event was in, as the expectation that nothing moved.
func partitionsOf(events map[int64]eventRow) map[int64]string {
	held := make(map[int64]string, len(events))
	for id, row := range events {
		held[id] = row.partition
	}
	return held
}

// TestTheConflictingLockLetsTheMoveOutRunAndStopsTheCreate is the precondition the case above
// declares. Without it the timeout could be landing on the drain's first statement, and every
// unchanged assertion would be satisfied by a drain that never started -- so the case named for a
// mid-drain failure would assert nothing about one.
//
// It drives the drain's own statements rather than a second expression of them. LOCK TABLE ONLY is
// what makes the distinction visible: it takes the parent and not its partitions, so the leaf
// statements below run and the create -- which needs the parent (M5) -- does not.
func TestTheConflictingLockLetsTheMoveOutRunAndStopsTheCreate(t *testing.T) {
	skipIfShort(t)

	blocked := aBlockedEventLog(t)
	defer restoreToSnapshot(t)

	work, err := drainFor(blocked.cfg, blocked.blocked)
	if err != nil {
		t.Fatalf("render the names of a drain of %s: %v", extentOf(blocked.blocked), err)
	}
	release := holdTheEventLogLocked(t, blocked.pool)
	defer release()

	tx, err := blocked.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin the transaction the move-out runs in: %v", err)
	}
	defer tx.Rollback(t.Context())
	mustExecOn(t, tx, lockTimeoutStatement(conflictingBound))

	for _, statement := range []string{work.queue.liftStatement(), work.queue.removeStatement(),
		work.events.liftStatement(), work.events.removeStatement()} {
		if _, err := tx.Exec(t.Context(), statement, blocked.blocked.From, blocked.blocked.To); err != nil {
			t.Fatalf("%s was refused while the conflicting lock was held: %v -- the drain would give "+
				"up before moving anything, and the mid-drain case asserts nothing", statement, err)
		}
	}
	if _, err := tx.Exec(t.Context(),
		createPartition(work.marked.target, work.parent, work.ranged)); err == nil {
		t.Fatal("the create succeeded while an ACCESS EXCLUSIVE lock on the parent was held, so the " +
			"refusal the case above measures is not the one this fixture sets up")
	} else if !strings.Contains(err.Error(), "lock timeout") {
		t.Errorf("the create was refused with %v rather than at the lock timeout", err)
	}
}
