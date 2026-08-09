//go:build integration

package schema

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 40's *drop* branch. D3 splits that criterion in three -- the mechanism is
// Step 8's, the create is Step 11's and the drop is this step's -- and no owning step may cite
// another's row, so the fixture, the pass and every assertion below are this step's own. What it
// takes from Step 8 is two durations, and a number is not evidence.
//
// The availability clause is the half that matters to a customer. M5 measured a drop taking ACCESS
// EXCLUSIVE on the parent and an insert into an *unrelated* partition blocking while it is held,
// through the parent and directly against the leaf, with no escape hatch. Since enqueue runs inside
// the customer's own transaction, the bound is what stands between a contended drop and a stalled
// production application -- so both routes are measured here rather than the one that first comes to
// mind.

// anInsertRoute is one way a customer's write reaches an unrelated partition. Both are timed,
// because measurement shows both block and an assertion over one of them would leave the other's
// unbounded wait unasserted.
type anInsertRoute struct {
	name string
	// into is the relation the statement names: the parent, or the leaf itself.
	into func(t *testing.T, unrelated Range) string
}

var theInsertRoutes = []anInsertRoute{
	{
		name: "through the parent", into: func(t *testing.T, _ Range) string {
			return mustQualify(t, harnessSchema, TableEvents)
		},
	},
	{
		name: "directly against the leaf", into: func(t *testing.T, unrelated Range) string {
			return mustQualify(t, harnessSchema, unrelated.Name)
		},
	},
}

// aTimedInsert is how long one route took and what it answered.
type aTimedInsert struct {
	route   string
	took    time.Duration
	refused error
}

// insertIntoUnrelatedPartition writes one event into a partition the drop does not name, on a
// connection of its own, and answers with how long the write waited. It reports rather than fails,
// because it runs on a goroutine of its own and a t.Fatalf there would leave the pass unfinished.
func insertIntoUnrelatedPartition(
	t *testing.T, cfg config.Config, route anInsertRoute,
	unrelated Range,
) aTimedInsert {

	pool := anotherReplica(t, cfg)
	target := route.into(t, unrelated)
	at := unrelated.From.Add(unrelated.To.Sub(unrelated.From) / 2)

	began := time.Now()
	_, err := pool.Exec(
		t.Context(), "INSERT INTO "+target+
			" (listener, table_name, operation, payload, txid, occurred_at)"+
			" VALUES ('orders', 'public.orders', 'INSERT', '{}'::jsonb, pg_current_xact_id(), $1)", at,
	)
	return aTimedInsert{route: route.name, took: time.Since(began), refused: err}
}

// TestADropMeetingAConflictingLockAbandonsWithinItsBoundAndChangesNothing is criterion 40's drop
// half. The classification is the evidence and the clock only corroborates it: a wall-clock
// assertion read as primary evidence is flaky on a loaded machine, and a flake here reads as an
// infrastructure problem rather than as the wrong assertion it is.
func TestADropMeetingAConflictingLockAbandonsWithinItsBoundAndChangesNothing(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	interval := cfg.Retention.PartitionInterval
	now := theClockOf(t, pool)
	expired := rangesEndingBefore(now.Add(-cfg.Retention.Keep), interval, 1)
	unrelated := rangeEndingAt(now.Add(interval), interval)
	plantMarkedPartitions(t, pool, append(slices.Clone(expired), unrelated))

	before := partitionInventory(t, pool)
	watching := anotherReplica(t, cfg)
	release := holdTheEventLogLocked(t, pool)

	report, elapsed, inserts := aContendedPass(t, pool, cfg, watching, unrelated, release)

	if !errors.Is(report.refused, ErrLockTimeout) {
		t.Fatalf(
			"the drop answered %v, want %v: a conflicting ACCESS EXCLUSIVE lock on %s was held "+
				"for the whole attempt", report.refused, ErrLockTimeout, TableEvents,
		)
	}
	if want := (Result{Outcome: OutcomeFailed}); report.settled != want {
		t.Errorf("the pass reported %+v, want %+v", report.settled, want)
	}
	if want := (statsReading{failures: 1}); report.stats != want {
		t.Errorf(
			"the pass recorded %+v, want %+v: one range it could not drop, and nothing on the "+
				"counter an operator alerts on for a retention undelivered events are holding",
			report.stats, want,
		)
	}
	assertBoundedBy(t, "the drop", elapsed, conflictingBound)
	for _, insert := range inserts {
		if insert.refused != nil {
			t.Errorf(
				"an insert %s answered %v, and it names a partition the drop does not",
				insert.route, insert.refused,
			)
		}
		assertBoundedBy(t, "an insert "+insert.route, insert.took, conflictingBound)
	}
	// Read after the release, because the bound-reading half of an inventory would itself wait on
	// the lock this episode is about.
	assertInventoryUnchanged(t, before, partitionInventory(t, pool), "a drop that timed out")
}

// aContendedPass runs one retention pass while both insert routes are issued against a partition it
// does not name, and answers what each of the three took. The inserts start only once the drop is
// queued, so they measure a wait behind a real ACCESS EXCLUSIVE request rather than an empty lock
// table.
//
// The conflicting lock is released here, the moment the pass gives up, and that placement is the
// measurement rather than housekeeping. The property is that *this pass* extends a customer's wait
// by no more than its own bound: the third party's lock would block those inserts whatever this
// package did, so it is held for exactly as long as the pass runs and no longer. Releasing it after
// the inserts were collected would instead deadlock, since neither can finish before the other.
func aContendedPass(
	t *testing.T, pool *pgxpool.Pool, cfg config.Config, watching *pgxpool.Pool,
	unrelated Range, release func(),
) (passReport, time.Duration, []aTimedInsert) {

	var report passReport
	passDone := make(chan time.Duration, 1)
	go func() {
		began := time.Now()
		report = oneRetentionPass(t, pool, cfg, Options{LockTimeout: conflictingBound})
		passDone <- time.Since(began)
	}()

	waitUntilBackendsAreQueuedOn(t, watching, mustQualify(t, harnessSchema, TableEvents), 1)
	written := make(chan aTimedInsert, len(theInsertRoutes))
	for _, route := range theInsertRoutes {
		go func() { written <- insertIntoUnrelatedPartition(t, cfg, route, unrelated) }()
	}

	elapsed := <-passDone
	release()

	inserts := make([]aTimedInsert, 0, len(theInsertRoutes))
	for range theInsertRoutes {
		inserts = append(inserts, <-written)
	}
	return report, elapsed, inserts
}

// assertBoundedBy is the corroborating clock check, generous on purpose: it exists to catch a wait
// that was never bounded at all, not to measure scheduling.
func assertBoundedBy(t *testing.T, what string, took, bound time.Duration) {
	t.Helper()

	t.Logf("%s took %s under a %s bound", what, took, bound)
	if took > bound+corroborationMargin {
		t.Errorf(
			"%s waited %s, more than its %s bound plus %s of slack; an unbounded wait here is "+
				"a stalled production application, because enqueue runs inside the customer's own "+
				"transaction", what, took, bound, corroborationMargin,
		)
	}
}
