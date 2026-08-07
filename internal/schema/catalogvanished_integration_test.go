//go:build integration

package schema

import (
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file constructs, deterministically, the race the three-replica retention this package
// supports produces by chance: a partition dropped between the scan that lists it and the call that
// renders its bound. Hunting for it is not an option -- it was measured at roughly one full tagged
// run in eight -- so the two halves are driven apart instead, by freezing this transaction's
// snapshot before the drop commits. The scan then still sees the partition and the bound resolves
// afresh against a catalog it has already left, which is the same pair a lost race produces at READ
// COMMITTED inside a single statement.

// theVanishedPairQuery counts the children of one parent that answer the pair cataloginventory.go
// skips on: a row that declares a partition bound, and no bound rendered for it. It is written out
// here rather than shared with the observation, the way catalogboundshapes_integration_test.go
// writes its own reading, because the single-authority scan ranges over production sources only.
const theVanishedPairQuery = `
SELECT count(*)
FROM pg_catalog.pg_inherits AS attachment
JOIN pg_catalog.pg_class AS child ON child.oid = attachment.inhrelid
JOIN pg_catalog.pg_class AS parent ON parent.oid = attachment.inhparent
JOIN pg_catalog.pg_namespace AS parentns ON parentns.oid = parent.relnamespace
WHERE parentns.nspname = $1 AND parent.relname = $2
  AND child.relpartbound IS NOT NULL
  AND pg_get_expr(child.relpartbound, child.oid) IS NULL`

// aSnapshotOutlivingADrop plants the required partitions, freezes a snapshot that still holds all of
// them, drops one on another connection, and answers with the frozen reader and the range it lost.
//
// The snapshot is fixed by a statement of its own before the drop: a repeatable-read transaction
// takes its snapshot at its first statement and not at BEGIN, so a transaction that had not yet run
// one would take a snapshot after the drop and see nothing to skip.
func aSnapshotOutlivingADrop(t *testing.T, pool *pgxpool.Pool) (pgx.Tx, Range, []Range) {
	t.Helper()

	required := RequiredRanges(theObservedInstant, theObservedRetention)
	plantPartitions(t, pool, harnessSchema, required)

	frozen, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatalf("begin the transaction whose snapshot outlives the drop: %v", err)
	}
	t.Cleanup(func() { frozen.Rollback(t.Context()) })

	var fixed int
	if err := frozen.QueryRow(t.Context(), "SELECT 1").Scan(&fixed); err != nil {
		t.Fatalf("fix the snapshot before the drop: %v", err)
	}

	// On the pool, so it commits while the reader above goes on seeing the partition. It takes no
	// lock the frozen transaction holds, which has read nothing but a constant.
	mustExecOn(t, pool, "DROP TABLE "+mustQualify(t, harnessSchema, required[0].Name))
	return frozen, required[0], required[1:]
}

// assertTheRaceHappened is the precondition each case here declares, measured rather than assumed. A
// fixture can drift out of its own contract -- a snapshot taken a moment too late holds no vanished
// partition at all -- and a case that then ran down the ordinary path would report the skip as
// working while never reaching it. Both cases below pass every one of their assertions on a snapshot
// that froze too late, which is exactly why neither may assume it did not.
//
// The count is the caller's, because the two cases drop a different number of partitions and a
// shared "at least one" would let either of them satisfy the other's contract.
func assertTheRaceHappened(t *testing.T, frozen pgx.Tx, want int) {
	t.Helper()

	var vanished int
	err := frozen.QueryRow(t.Context(), theVanishedPairQuery, harnessSchema, TableEvents).Scan(&vanished)
	if err != nil {
		t.Fatalf("count the children whose bound no longer renders: %v", err)
	}
	if vanished != want {
		t.Fatalf("%d children declare a bound the server no longer renders, want exactly %d; this "+
			"snapshot did not hold the partitions this case dropped, so nothing below reaches the "+
			"skip", vanished, want)
	}
}

// TestAPartitionDroppedBetweenTheScanAndTheBoundReadIsSkippedRatherThanAbortingTheInventory is the
// defect. Before the fix the whole observation was refused -- one partition another replica had
// already dropped cost a maintenance or retention pass every other partition it could see -- and
// under the three-replica retention this package supports that is the expected case rather than an
// anomaly.
func TestAPartitionDroppedBetweenTheScanAndTheBoundReadIsSkippedRatherThanAbortingTheInventory(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	frozen, vanished, survivors := aSnapshotOutlivingADrop(t, pool)
	assertTheRaceHappened(t, frozen, 1)

	found, err := observePartitions(t.Context(), frozen, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe across a partition dropped since the scan listed it: %v", err)
	}

	// The catalog answers in name order and the arithmetic in time order, so both are put in one
	// order before they are compared per element rather than for membership.
	observed := slices.Clone(found.Bounded)
	slices.SortFunc(observed, func(a, b Range) int { return a.From.Compare(b.From) })

	if len(observed) != len(survivors) {
		t.Fatalf("the observation returned %d bounded partitions, want the %d that did not vanish",
			len(observed), len(survivors))
	}
	for i, want := range survivors {
		if got := observed[i]; got.Name != want.Name || !got.From.Equal(want.From) ||
			!got.To.Equal(want.To) {
			t.Errorf("partition %d was read back as %s [%s, %s), want %s [%s, %s)",
				i, got.Name, got.From, got.To, want.Name, want.From, want.To)
		}
	}
	if slices.ContainsFunc(observed, vanished.sameExtentAs) {
		t.Errorf("%s was reported as covering %s, and it has been dropped; retention offered a "+
			"partition that is not there decides against a world that does not exist",
			vanished.Name, extentOf(vanished))
	}
	if found.Default != PartitionDefault {
		t.Errorf("the observation tagged %q as the DEFAULT partition, want %q; a vanished sibling "+
			"must not cost the pass its write-availability net", found.Default, PartitionDefault)
	}
}

// TestAVanishedDefaultPartitionIsSkippedIntoNoNetRatherThanAWrongOne is the one input class the skip must
// not smooth over. The DEFAULT partition is the write-availability net every unrouted row lands in
// (criterion 36), and a pass that carried on creating partitions in a log whose net had gone would be the
// fail-open this observation exists to prevent. Skipping it is right -- it really is gone -- and leaving
// Default empty is what makes the caller's own guard fire, so the consequence is asserted here and not
// only the state.
func TestAVanishedDefaultPartitionIsSkippedIntoNoNetRatherThanAWrongOne(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	frozen, _, _ := aSnapshotOutlivingADrop(t, pool)
	mustExecOn(t, pool, "DROP TABLE "+mustQualify(t, harnessSchema, PartitionDefault))

	// Two: the bounded partition the helper dropped, and the DEFAULT partition dropped just now.
	assertTheRaceHappened(t, frozen, 2)

	found, err := observePartitions(t.Context(), frozen, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe across a dropped DEFAULT partition: %v", err)
	}
	if found.Default != "" {
		t.Fatalf("the observation tagged %q as the DEFAULT partition after it was dropped; a net "+
			"that is gone must not be reported as one that is there", found.Default)
	}

	if _, err := defaultPartitionRows(t.Context(), frozen, harnessSchema, found.Default); err == nil {
		t.Error("counting the rows of a DEFAULT partition the observation could not name succeeded; " +
			"a log with no write-availability net is not one to go on creating partitions in")
	}
}

// TestAPartitionedParentAdmitsNoChildThatDeclaresNoBound is the measurement cataloginventory.go's
// boundUndeclared cites, pinned against the running server rather than left in a comment. It is
// what bounds the skip: under a partitioned parent every child declares a bound, so a row that
// declares none says the parent is not partitioned, and only a row that declares one and renders
// none can be a lost race.
func TestAPartitionedParentAdmitsNoChildThatDeclaresNoBound(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)

	_, err := pool.Exec(t.Context(), "CREATE TABLE "+mustQualify(t, harnessSchema, "classic_child")+
		" () INHERITS ("+mustQualify(t, harnessSchema, TableEvents)+")")
	if err == nil {
		t.Fatal("the server attached a classic inheritance child to a partitioned parent; a child " +
			"declaring no bound is then reachable under our own event log, and the observation " +
			"would skip it as a lost race rather than refusing it")
	}
	if !strings.Contains(err.Error(), "cannot inherit from partitioned table") {
		t.Errorf("the server refused with %v, and boundUndeclared is written against `cannot "+
			"inherit from partitioned table`", err)
	}
}
