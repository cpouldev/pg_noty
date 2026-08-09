//go:build integration

package source

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The per-write half of the enqueue claim, and the helpers its crossing half reads. The enqueue is
// one statement writing two rows, so a watched write producing anything other than one event row and
// one queue row means the CTE was restructured. The queue row has to carry the event's *own*
// occurred_at, because internal/schema's key -- (event_id, occurred_at) REFERENCES events
// (id, occurred_at) -- resolves against whichever partition that value routes to, and it is written pending,
// unattempted and unleased.
//
// The crossing those same claims turn on -- events in *different* partitions of the log, where a
// mismatched occurred_at stops satisfying the key -- is
// TestEnqueueCrossesTheGridBoundaryWithEveryQueueRowResolving in
// enqueuepartition_integration_test.go, which reuses every helper declared here rather than
// restating it.

// foreignKeyViolation is PostgreSQL's SQLSTATE for a key naming no parent row: class 23, code 503.
// It is read through revokeexecute_integration_test.go's sqlStateOf, so "the server refused" cannot
// be satisfied by a dropped connection or by an error raised in Go.
const foreignKeyViolation = "23503"

// theEventLogInWriteOrder reads the whole log rather than a count, so a caller asserting "exactly
// one" reads the population it is about to assert over. The partition comes from pg_class.relname and
// not from tableoid::regclass::text, whose rendering depends on search_path and would not compare
// against a bare partition name. Both relations are aliased and every column qualified: an
// unqualified tableoid inside the subquery resolves to pg_class's own, which is a legal answer naming
// the wrong table, so every row would report the partition `pg_class`.
const theEventLogInWriteOrder = "SELECT e.id, e.occurred_at, " +
	"(SELECT c.relname FROM pg_class c WHERE c.oid = e.tableoid) FROM noty.events e ORDER BY e.id"

// enqueued is one event as the generated trigger wrote it, with the leaf partition it landed in.
type enqueued struct {
	id         int64
	occurredAt time.Time
	partition  string
}

func eventsWritten(t *testing.T, pool *pgxpool.Pool) []enqueued {
	t.Helper()
	rows, err := pool.Query(t.Context(), theEventLogInWriteOrder)
	if err != nil {
		t.Fatalf("read the event log: %v", err)
	}
	defer rows.Close()
	var written []enqueued
	for rows.Next() {
		var one enqueued
		if err := rows.Scan(&one.id, &one.occurredAt, &one.partition); err != nil {
			t.Fatalf("scan an event row: %v", err)
		}
		written = append(written, one)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the event log: %v", err)
	}
	return written
}

// installEnqueueTarget is the fixture both files write through: one watched table carrying a
// generated insert trigger, over internal/schema's own migrated event log and composite key.
func installEnqueueTarget(t *testing.T, pool *pgxpool.Pool, table string) {
	t.Helper()
	mustExecOn(t, pool, "CREATE TABLE public."+table+" (id int)")
	installInsertTrigger(t, pool, "noty", "enqueue", table)
}

// assertOneFreshPendingQueueRow is the enqueue claim for one event: the queue row carries the
// event's own occurred_at, and it is pending with no attempt spent and neither lease field set.
func assertOneFreshPendingQueueRow(t *testing.T, pool *pgxpool.Pool, event enqueued) {
	t.Helper()
	var occurredAt time.Time
	var status string
	var attempts int
	var leasedUntil *time.Time
	var leasedBy *string
	if err := pool.QueryRow(t.Context(), "SELECT occurred_at, status, attempts, leased_until, "+
		"leased_by FROM noty.event_queue WHERE event_id = $1", event.id).
		Scan(&occurredAt, &status, &attempts, &leasedUntil, &leasedBy); err != nil {
		t.Fatalf("event %d in partition %s has no queue row of its own: %v",
			event.id, event.partition, err)
	}
	if !occurredAt.Equal(event.occurredAt) {
		t.Errorf("the queue row for event %d carries occurred_at %v where the event in partition %s "+
			"carries %v; the key resolves against whichever partition the queue value routes to",
			event.id, occurredAt, event.partition, event.occurredAt)
	}
	if status != "pending" || attempts != 0 {
		t.Errorf("the queue row for event %d is %q with %d attempts, want pending with 0",
			event.id, status, attempts)
	}
	if leasedUntil != nil || leasedBy != nil {
		t.Errorf("the queue row for event %d was written leased until %v by %v; an enqueue holds no "+
			"lease", event.id, leasedUntil, leasedBy)
	}
}

// assertTheCompositeKeyHoldsTheEvent proves the foreign key by execution rather than by reading the
// DDL: the server refuses to delete the event the queue row names out of the partition it landed in,
// and a key that did not resolve there would let the delete through.
func assertTheCompositeKeyHoldsTheEvent(t *testing.T, pool *pgxpool.Pool, event enqueued) {
	t.Helper()
	_, err := pool.Exec(t.Context(), "DELETE FROM noty.events WHERE id = $1 AND occurred_at = $2",
		event.id, event.occurredAt)
	if got := sqlStateOf(err); got != foreignKeyViolation {
		t.Errorf("deleting event %d out of partition %s answered SQLSTATE %q (%v), want the server's "+
			"%s; the composite foreign key does not resolve in that partition",
			event.id, event.partition, got, err, foreignKeyViolation)
	}
}

// validatedForeignKeysFromQueueToLog counts the queue's validated foreign keys into the event log. A
// NOT VALID key would leave every row written before it unchecked, so convalidated is part of the
// question rather than a detail of it.
func validatedForeignKeysFromQueueToLog(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	return countOn(t, pool, "SELECT count(*) FROM pg_constraint WHERE contype = 'f' AND convalidated "+
		"AND conrelid = 'noty.event_queue'::regclass AND confrelid = 'noty.events'::regclass")
}

// TestEnqueueCarriesTheEventOccurredAtIntoTheQueue is the enqueue claim for a single watched write:
// exactly one event row and one queue row, the queue row's occurred_at equal to the event's, pending
// with zero attempts and no lease held, and the composite key validated and resolving.
func TestEnqueueCarriesTheEventOccurredAtIntoTheQueue(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	installEnqueueTarget(t, pool, "enqueue_target")
	mustExecOn(t, pool, `INSERT INTO public.enqueue_target VALUES (1)`)

	written := eventsWritten(t, pool)
	if len(written) != 1 {
		t.Fatalf("one watched INSERT wrote %d event rows, want exactly 1", len(written))
	}
	if queued := countOn(t, pool, "SELECT count(*) FROM noty.event_queue"); queued != 1 {
		t.Fatalf("one watched INSERT wrote %d queue rows, want exactly 1", queued)
	}
	if keys := validatedForeignKeysFromQueueToLog(t, pool); keys != 1 {
		t.Fatalf("the queue carries %d validated foreign keys into the event log, want exactly 1", keys)
	}
	assertOneFreshPendingQueueRow(t, pool, written[0])
	assertTheCompositeKeyHoldsTheEvent(t, pool, written[0])
}
