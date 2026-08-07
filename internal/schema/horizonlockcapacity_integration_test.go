//go:build integration

package schema

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is why horizon.go carries no bound derived from the server's lock table, measured rather
// than argued.
//
// The reasoning that suggested one is sound about PostgreSQL and wrong about this package. A query
// against a partitioned parent takes a lock per partition, and the lock table is sized at startup
// from max_locks_per_transaction * (max_connections + max_prepared_transactions), so such a query has
// a partition ceiling past which the server answers `out of shared memory` (SQLSTATE 53200).
// Measured out of band on the pinned image with that product set to 200, `SELECT count(*) FROM events`
// over 401 partitions raises exactly that; the same server ran this package's whole retention
// transaction over 3,000 partitions in 22 locks.
//
// The reason is three properties of the statements themselves, and each is one word from being false:
// lockTheEventLog writes LOCK TABLE ONLY, whose ONLY is what stops the lock recursing into every
// partition; catalog.go counts the DEFAULT partition by name and never through the parent; and each
// create, detach, drop and drain names one partition and runs in its own transaction. A bound in
// horizon.go would therefore refuse configurations this package serves -- an exclusion guard that
// fires too widely. What is asserted here instead is
// the independence itself, so the day one of those three properties changes, this fails rather than
// the bound quietly becoming necessary.

// theBackendsLockCount is how many locks the transaction asking holds. It is asked inside the
// transaction because locks are released at commit, so a reading taken afterwards would report the same
// nothing for every implementation.
const theBackendsLockCount = `SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid()`

// theLockTableCapacity is the arithmetic PostgreSQL sizes its lock table by, read from the server it
// is about rather than assumed -- which is the whole reason a bound derived from it could not live
// in horizon.go, whose every constant is a property of PostgreSQL itself.
const theLockTableCapacity = `SELECT current_setting('max_locks_per_transaction')::int
  * (current_setting('max_connections')::int + current_setting('max_prepared_transactions')::int)`

// The two partition counts every statement is measured at. Ten times apart, so a statement taking one
// lock per partition differs by 180 and cannot be mistaken for measurement noise.
const (
	fewPartitions  = 20
	manyPartitions = 200
)

// statementsAimedAtTheEventLogCount is how many builders the measurement drives. It is pinned rather
// than derived from the list so that a builder joining it is reasoned about instead of silently
// widening what this file claims to have measured.
const statementsAimedAtTheEventLogCount = 7

// TestEveryStatementAimedAtTheEventLogTakesTheSameLocksAtAnyPartitionCount is the property the absent
// bound rests on. The unpruned parent query beside it is the positive control: without it a scan that
// measured nothing at all would report perfect independence, which is also what it reports when the
// package is independent.
func TestEveryStatementAimedAtTheEventLogTakesTheSameLocksAtAnyPartitionCount(t *testing.T) {
	skipIfShort(t)

	few := lockFootprintOver(t, fewPartitions)
	many := lockFootprintOver(t, manyPartitions)

	t.Logf("lock table capacity %d; this package holds %d locks over %d partitions and %d over %d; "+
		"an unpruned query against the parent holds %d and %d",
		many.capacity, few.thisPackage, fewPartitions, many.thisPackage, manyPartitions,
		few.unprunedParentQuery, many.unprunedParentQuery)

	if few.thisPackage != many.thisPackage {
		t.Errorf("this package's statements hold %d locks over %d partitions and %d over %d; a "+
			"footprint that grows with the partition count gives this package a ceiling of its own, "+
			"and horizon.go carries no bound for one",
			few.thisPackage, fewPartitions, many.thisPackage, manyPartitions)
	}
	assertTheInstrumentCanSeeAFootprintGrow(t, few, many)
}

// assertTheInstrumentCanSeeAFootprintGrow is the control, and it is an inequality because what it
// establishes is that the measurement is capable of reporting growth at all -- the exact slope is
// the server's business and pinning it would pin a plan shape nothing promises.
func assertTheInstrumentCanSeeAFootprintGrow(t *testing.T, few, many lockFootprint) {
	t.Helper()

	added := int64(manyPartitions - fewPartitions)
	if grew := many.unprunedParentQuery - few.unprunedParentQuery; grew < added {
		t.Fatalf("an unpruned query against the parent holds %d locks over %d partitions and %d over "+
			"%d, a rise of %d for %d more partitions; it is supposed to take at least one lock apiece, "+
			"so this measurement cannot see a per-partition footprint and the equality above passes "+
			"whatever the package does", few.unprunedParentQuery, fewPartitions,
			many.unprunedParentQuery, manyPartitions, grew, added)
	}
	if many.capacity <= 0 {
		t.Errorf("the server reports a lock table of %d entries, so the ceiling an unpruned query "+
			"has could not be derived and the claim that this package has none rests on nothing",
			many.capacity)
	}
}

// lockFootprint is what one partition count costs: the locks this package's own statements hold
// together, the locks one unpruned query against the parent holds, and the table both draw from.
type lockFootprint struct {
	thisPackage, unprunedParentQuery, capacity int64
}

// lockFootprintOver plants an event log of the given size and measures all three.
func lockFootprintOver(t *testing.T, partitions int) lockFootprint {
	t.Helper()

	pool := eventLogFixture(t)
	plantPartitions(t, pool, harnessSchema,
		RequiredRanges(theObservedInstant, retentionOf(time.Hour, time.Duration(partitions-1)*time.Hour,
			72*time.Hour)))

	var measured lockFootprint
	if err := pool.QueryRow(t.Context(), theLockTableCapacity).Scan(&measured.capacity); err != nil {
		t.Fatalf("read the lock table capacity: %v", err)
	}
	measured.thisPackage = locksHeldAfter(t, pool, statementsAimedAtTheEventLog(t))
	measured.unprunedParentQuery = locksHeldAfter(t, pool, []probeStatement{
		{sql: "SELECT count(*) FROM " + mustQualify(t, harnessSchema, TableEvents)}})
	return measured
}

// locksHeldAfter runs the statements in one transaction and answers how many locks it then holds.
// The transaction is rolled back, so a measurement changes nothing the next one reads.
func locksHeldAfter(t *testing.T, pool *pgxpool.Pool, statements []probeStatement) int64 {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin the transaction the locks are counted in: %v", err)
	}
	defer tx.Rollback(t.Context())

	for _, statement := range statements {
		if _, err := tx.Exec(t.Context(), statement.sql, statement.arguments...); err != nil {
			t.Fatalf("exec %s: %v", statement.sql, err)
		}
	}

	var held int64
	if err := tx.QueryRow(t.Context(), theBackendsLockCount).Scan(&held); err != nil {
		t.Fatalf("count the locks this transaction holds: %v", err)
	}
	return held
}

// probeStatement is one statement the measurement runs, with the values a builder left as bind
// parameters. The drain's lift carries the two the half-open range is selected by (M11).
type probeStatement struct {
	sql       string
	arguments []any
}

// statementsAimedAtTheEventLog is every statement this package issues against the event log or one of
// its partitions, built through the production builders so that a change to one of them is measured
// here rather than paraphrased. Statements aimed at the delivery queue are absent because the queue
// is not partitioned (migration 2), and catalog reads because they lock no partition.
//
// The list is what this measurement cannot close on its own: "aims at the event log" is semantic, so
// a seventh builder joining the package is invisible to it. The size pin is the mitigation, exactly
// as theInstantToRangeSurfaceSize is for the partition-deciding proxy.
func statementsAimedAtTheEventLog(t *testing.T) []probeStatement {
	t.Helper()

	cfg := configWithSchema(harnessSchema)
	ranged := rangeAt(gridIndex(theObservedInstant.Add(-500*time.Hour), time.Hour), time.Hour)
	parent := mustQualify(t, harnessSchema, TableEvents)
	target := mustQualify(t, harnessSchema, ranged.Name)

	work, err := drainFor(cfg, ranged)
	if err != nil {
		t.Fatalf("render the drain of %s: %v", extentOf(ranged), err)
	}

	statements := []probeStatement{
		{sql: lockTheEventLog(parent)},
		{sql: countRowsPrefix + mustQualify(t, harnessSchema, PartitionDefault)},
		{sql: work.events.liftStatement(), arguments: []any{ranged.From, ranged.To}},
		{sql: createPartition(target, parent, ranged)},
		{sql: work.events.restoreStatement()},
		{sql: detachPartition(parent, target)},
		{sql: dropPartition(target)},
	}
	if len(statements) != statementsAimedAtTheEventLogCount {
		t.Fatalf("%d statements are measured and %d are declared; update the count with the builder "+
			"that joined or left, or a statement aimed at the event log is outside the measurement",
			len(statements), statementsAimedAtTheEventLogCount)
	}
	return statements
}
