//go:build integration

package schema

import (
	"strings"
	"testing"
)

// This file is the refusals: the ones the drain makes, and the one the server makes at it.
//
// It reaches each of the drain's fail-closed refusals with the state a caller reaches it with, and
// asserts the reason rather than only the refusal. Each row is built to trip one condition alone,
// so a condition silently dropped fails the row named for it rather than passing on a neighbour's
// refusal -- except the last, which trips two on purpose. The two cases at the foot are the other
// directions: what the server refuses the drain when it drops the clause that carries the ids
// across the move, and the drain's own conservation guard, reached directly because no server
// reaches it.

// withoutADefaultPartition takes the event log's write-availability net away, which is a state this
// package's own code never produces: criterion 29 forbids dropping or detaching the DEFAULT
// partition under any operation, so only a DBA can put a log into it. The queue and the events go
// first because the composite foreign key depends on the partition holding their rows, and the
// detach goes before the drop because the key depends on the partition itself.
func withoutADefaultPartition(t *testing.T, blocked blockedLog) {
	t.Helper()

	events := mustQualify(t, harnessSchema, TableEvents)
	mustExecOn(t, blocked.pool, "DELETE FROM "+mustQualify(t, harnessSchema, TableEventQueue))
	mustExecOn(t, blocked.pool, "DELETE FROM "+events)
	mustExecOn(t, blocked.pool, "ALTER TABLE "+events+" DETACH PARTITION "+
		mustQualify(t, harnessSchema, PartitionDefault))
}

// TestADrainThatCannotBeMadeSafeIsRefusedByItsOwnReason is the fail-closed half of the drain.
//
// The last row is the one that pins the order whyUnrepairable documents: it violates *both* rules
// at once -- a covered range in a log with no DEFAULT partition -- so only a reader that tests the
// missing net first answers "no DEFAULT partition". Until such a row exists both orders answer
// identically on every input, and reversing the order would change which reason an operator is
// shown while the suite stayed green.
func TestADrainThatCannotBeMadeSafeIsRefusedByItsOwnReason(t *testing.T) {
	skipIfShort(t)

	for _, tc := range []struct {
		name, wants string
		ranged      func(blockedLog) Range
		before      func(*testing.T, blockedLog)
	}{
		{name: "a range a partition already covers", wants: "already covers",
			ranged: func(blocked blockedLog) Range { return blocked.wanted[1] }},
		{name: "a range whose upper bound is not after its lower bound", wants: "selects no row",
			ranged: func(blocked blockedLog) Range {
				return Range{From: blocked.blocked.From, To: blocked.blocked.From, Name: blocked.blocked.Name}
			}},
		{name: "an event log carrying no DEFAULT partition", wants: "no DEFAULT partition",
			ranged: func(blocked blockedLog) Range { return blocked.blocked }, before: withoutADefaultPartition},
		{name: "a covered range in a log with no DEFAULT partition", wants: "no DEFAULT partition",
			ranged: func(blocked blockedLog) Range { return blocked.wanted[1] }, before: withoutADefaultPartition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocked := aBlockedEventLog(t)
			defer restoreToSnapshot(t)
			if tc.before != nil {
				tc.before(t, blocked)
			}
			inventory, held := partitionInventory(t, blocked.pool), eventInventory(t, blocked.pool)

			done, err := RepairDefaultPartition(t.Context(), blocked.pool, blocked.cfg, Options{},
				tc.ranged(blocked))
			if err == nil {
				t.Fatalf("the drain reported %+v over a state it cannot make safe", done)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("the drain was refused with %v, which does not say %q", err, tc.wants)
			}
			assertInventoryUnchanged(t, inventory, partitionInventory(t, blocked.pool), "a refused drain")
			assertLandedIn(t, blocked.pool, held, partitionsOf(held))
		})
	}
}

// TestTheReInsertIsRefusedWithoutOverridingSystemValue is the negative control behind SC-6. That the
// ids survive is asserted end to end in repairrecovery_integration_test.go; this is why the clause
// is not a preference. Measured on 17.10, events.id is GENERATED ALWAYS, so the same statement
// without it is refused outright -- while one written to dodge that by naming the other columns
// would be accepted and would renumber every row, orphaning the queue rows pointing at them (M1).
func TestTheReInsertIsRefusedWithoutOverridingSystemValue(t *testing.T) {
	skipIfShort(t)

	blocked := aBlockedEventLog(t)
	defer restoreToSnapshot(t)

	// The clause is named as the literal SC-6 writes rather than as the constant holding it. A
	// control that looked for the constant's own value would be satisfied by an empty constant,
	// which is precisely the defect it exists to catch.
	const theClause = "OVERRIDING SYSTEM VALUE "

	work, err := drainFor(blocked.cfg, blocked.blocked)
	if err != nil {
		t.Fatalf("render the names of a drain of %s: %v", extentOf(blocked.blocked), err)
	}
	if overridingSystemValue != theClause {
		t.Errorf("the drain writes %q where SC-6 names %q", overridingSystemValue, theClause)
	}
	if !strings.Contains(work.events.restoreStatement(), theClause) {
		t.Fatalf("the re-insert is written as %s, which carries no %s",
			work.events.restoreStatement(), theClause)
	}

	tx, err := blocked.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin the transaction the control runs in: %v", err)
	}
	defer tx.Rollback(t.Context())

	mustExecOn(t, tx, work.events.liftStatement(), blocked.blocked.From, blocked.blocked.To)
	withoutTheClause := strings.Replace(work.events.restoreStatement(), theClause, "", 1)
	if _, err := tx.Exec(t.Context(), withoutTheClause); err == nil {
		t.Fatalf("%s was accepted; the clause it drops is then inert and a drain could lose it "+
			"without any test noticing", withoutTheClause)
	} else if !strings.Contains(err.Error(), "cannot insert a non-DEFAULT value into column") {
		t.Errorf("%s was refused with %v, and the refusal this clause answers is the identity "+
			"column's", withoutTheClause, err)
	}
}

// TestARowThatDoesNotArriveIsRefusedRatherThanCommitted reaches the drain's own conservation guard
// with the state it exists for: a count that does not add up. No real server reaches it -- the
// re-insert puts back exactly what the holding table holds -- so the branch is driven directly, and
// an edit that returned silently instead of refusing fails here rather than committing a partial
// move of the customer's rows.
//
// liftedOut carries the same guard over the copy and the delete, and it cannot be reached this way:
// both of its statements read one source inside one transaction, so nothing this package can build
// makes them disagree. The rule the two share is the one asserted here.
func TestARowThatDoesNotArriveIsRefusedRatherThanCommitted(t *testing.T) {
	skipIfShort(t)

	blocked := aBlockedEventLog(t)
	defer restoreToSnapshot(t)

	work, err := drainFor(blocked.cfg, blocked.blocked)
	if err != nil {
		t.Fatalf("render the names of a drain of %s: %v", extentOf(blocked.blocked), err)
	}
	tx, err := blocked.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin the transaction the guard runs in: %v", err)
	}
	defer tx.Rollback(t.Context())

	lifted, err := liftedOut(t.Context(), tx, work.queue, blocked.blocked)
	if err != nil {
		t.Fatalf("lift %s's rows out: %v", TableEventQueue, err)
	}
	if lifted == 0 {
		t.Fatalf("no %s row was lifted out, so a count that disagrees with it says nothing",
			TableEventQueue)
	}

	switch err := putBack(t.Context(), tx, work.queue, lifted+1); {
	case err == nil:
		t.Fatalf("%d rows were put back where %d were claimed to have left, and the drain committed it",
			lifted, lifted+1)
	case !strings.Contains(err.Error(), "arrived in"):
		t.Errorf("the guard refused with %v, which does not say how many rows arrived", err)
	}
}
