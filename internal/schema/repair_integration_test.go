//go:build integration

package schema

import (
	"errors"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file holds the fixtures Step 15's container-backed cases are built on. The cases are in
// repairrecovery_ (criterion 30's chain, both ways), repairrange_ (which rows move and which do
// not) and repairatomicity_ (one transaction, and what a failure leaves behind);
// repairscan_test.go is the container-free half.
//
// Step 15's Expected Output names one integration file. This step is written across five for the
// reason Steps 1, 3 and 11 declared the same deviation: one file of each would breach the 200-line
// budget this package's own gate enforces.
//
// Every case here runs against the schema the *shipped corpus* builds, not against Step 9's
// hand-built event log. That is not a preference. Measured on 17.10 against migration 2's own DDL,
// removing an event from the DEFAULT partition while an event_queue row still points at it is
// refused outright:
//
//	update or delete on table "events_default" violates foreign key constraint
//	"event_queue_event_id_occurred_at_fkey1" on table "event_queue"
//
// so the composite foreign key AC 39 ratified is part of what the drain has to get right, and a
// two-column fixture carrying no queue could not reach it.

// blockedLog is the state criterion 30 begins in: events sitting in the DEFAULT partition for a
// range no partition covers, and the pass that has already refused that range.
type blockedLog struct {
	pool *pgxpool.Pool
	cfg  config.Config
	// blocked is the range those events belong to, and drained is the id of every one of them, in
	// the order they were planted.
	blocked Range
	drained []int64
	// wanted is the horizon the refused pass was given, so a later pass can be checked against the
	// same arithmetic rather than against what it finds on disk.
	wanted []Range
	// defaultPartition is the DEFAULT partition's identity before the drain. Identity and not
	// existence: one dropped and re-created exists just as much as the original and holds none of
	// its rows (criterion 29).
	defaultPartition int64
}

// aBlockedEventLog plants events in the DEFAULT partition for one range and runs the pass that
// refuses it, so every case below starts from the state the drain exists to clear.
//
// The two planted instants are the range's own lower bound and its interior. FROM is inclusive
// (M11), so a row written at exactly the bound belongs to this range and has to be drained with it;
// the interior row is what keeps the boundary row from being the only evidence.
func aBlockedEventLog(t *testing.T) blockedLog {
	t.Helper()

	pool := migratedSchema(t)
	cfg := harnessConfig(t)
	cfg.Retention = theMaintainedRetention

	before := theClockOf(t, pool)
	blocked := RequiredRanges(before, cfg.Retention)[0]
	drained := []int64{
		plantAndEnqueue(t, pool, blocked.From),
		plantAndEnqueue(t, pool, blocked.From.Add(cfg.Retention.PartitionInterval/2)),
	}
	identity, _ := identityOf(t, pool, harnessSchema, PartitionDefault)

	report := onePass(t, pool, cfg, Options{})
	wanted := theHorizonBetween(t, before, theClockOf(t, pool))
	assertRangeBlocked(t, pool, report, blocked, "the pass that met the planted rows")

	return blockedLog{
		pool: pool, cfg: cfg, blocked: blocked, drained: drained, wanted: wanted,
		defaultPartition: identity,
	}
}

// plantAndEnqueue writes one event and enqueues it, which is the shape internal/delivery's enqueue
// takes: the queue row carries the event's *own* occurred_at, because a mismatched value points the
// composite key at a different partition and the insert fails inside the customer's transaction.
//
// It composes Step 12's plantEvent rather than copying it, so the event log's column list stays
// declared in one place. What that planter does not write is the queue row, and the queue row is
// the whole reason this drain cannot simply delete an event. Each call is its own transaction, so
// every planted row carries a distinct txid and the whole-row comparison below can tell two rows
// apart.
func plantAndEnqueue(t *testing.T, pool *pgxpool.Pool, at time.Time) int64 {
	t.Helper()

	planted := plantEvent(t, pool, at)
	mustExecOn(
		t, pool, "INSERT INTO "+mustQualify(t, harnessSchema, TableEventQueue)+
			" (event_id, occurred_at, listener, status, attempts, next_attempt_at)"+
			" VALUES ($1, $2, 'a-listener', 'pending', 0, now())", planted.id, planted.occurredAt,
	)
	return planted.id
}

// eventRow is one event as this suite reads it back: the partition holding it now, and every column
// it carries rendered as one value.
//
// The whole row rather than the id alone, because a drain that preserved identifiers while
// corrupting payloads would satisfy an id comparison exactly. The partition is read separately for
// the same reason it is read at all -- it is the one thing the drain is meant to change.
type eventRow struct {
	partition string
	columns   string
}

// eventInventory is every event in the log, keyed on its identity. It carries no range predicate on
// purpose: a test selecting its subjects with the drain's own predicate would agree with a wrong
// one, so every assertion below names the row it means by the id it planted.
func eventInventory(t *testing.T, pool *pgxpool.Pool) map[int64]eventRow {
	t.Helper()

	// The tableoid is rendered through the same cast identityOf reads a name through, so neither can
	// disagree with the other about a search_path.
	rows, err := pool.Query(
		t.Context(), "SELECT id, tableoid::regclass::text, to_jsonb(e.*)::text"+
			" FROM "+mustQualify(t, harnessSchema, TableEvents)+" e",
	)
	if err != nil {
		t.Fatalf("read every event with the partition holding it: %v", err)
	}
	defer rows.Close()

	found := map[int64]eventRow{}
	for rows.Next() {
		var id int64
		var held eventRow
		if err := rows.Scan(&id, &held.partition, &held.columns); err != nil {
			t.Fatalf("read one event back: %v", err)
		}
		found[id] = held
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the events back: %v", err)
	}
	return found
}

// assertRangeBlocked is the recovery chain's first two links: the pass reports ErrDefaultBlocked,
// and it leaves the range uncovered. Both, because a pass that reported the condition and created
// the partition anyway would satisfy the first alone -- and because "never reports success while
// coverage remains absent" is the row that makes the repair's own claim falsifiable.
func assertRangeBlocked(
	t *testing.T, pool *pgxpool.Pool, report passReport, blocked Range,
	during string,
) {
	t.Helper()

	if !errors.Is(report.refused, ErrDefaultBlocked) {
		t.Fatalf(
			"%s answered %v, want %v: rows for %s were sitting in the DEFAULT partition",
			during, report.refused, ErrDefaultBlocked, extentOf(blocked),
		)
	}
	if report.settled.Outcome != OutcomeFailed {
		t.Fatalf(
			"%s reported %+v while %s was left uncovered; a pass reporting anything but failure "+
				"over an uncovered range is the quiet success criterion 30 forbids",
			during, report.settled, extentOf(blocked),
		)
	}
	if coversRange(t, pool, blocked) {
		t.Fatalf(
			"%s left %s covered by a partition, and the state this suite drains is one where it "+
				"is not", during, extentOf(blocked),
		)
	}
}

// coversRange answers the coverage question through plan.go's own membership rule -- by the
// observed *extent* and never by the name a statement wrote (M6) -- so a case cannot be satisfied
// under a rule the pass does not use.
func coversRange(t *testing.T, pool *pgxpool.Pool, ranged Range) bool {
	t.Helper()

	return len(rangesNotYetObserved([]Range{ranged}, observedBounds(t, pool))) == 0
}

// observedBounds is every bounded partition of the event log, read through the package's own
// observation authority so a case cannot see the world differently from the code under test.
func observedBounds(t *testing.T, pool *pgxpool.Pool) []Range {
	t.Helper()

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the partitions of %s: %v", TableEvents, err)
	}
	return found.Bounded
}
