//go:build integration

package schema

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Criterion 6's third fact: what the single identity sequence gives, and what no constraint gives.
// The event log's rows are planted here rather than in the queue's own file because the queue's
// composite foreign key means a queue row cannot exist without one.

// The two inserts below share their column list and their values, so a column added to one cannot
// drift from the other.
const (
	eventColumns = "listener, table_name, operation, payload, txid, occurred_at"
	eventValues  = "'a-listener', 'a_table', 'INSERT', '{}'::jsonb, pg_current_xact_id(), $1"
)

// plantedEvent is one row of the event log with the identity the server assigned it and the instant
// it is keyed by -- together, what a queue row must carry to compose the foreign key.
type plantedEvent struct {
	id         int64
	occurredAt time.Time
}

func plantEvent(t *testing.T, pool *pgxpool.Pool, at time.Time) plantedEvent {
	t.Helper()

	var planted plantedEvent
	err := pool.QueryRow(t.Context(), "INSERT INTO "+mustQualify(t, harnessSchema, TableEvents)+
		" ("+eventColumns+") VALUES ("+eventValues+") RETURNING id, occurred_at", at).
		Scan(&planted.id, &planted.occurredAt)
	if err != nil {
		t.Fatalf("plant an event at %s: %v", at, err)
	}
	return planted
}

// partitionsHoldingQuery names the partition each row with one id actually landed in, so "two rows
// exist" and "two rows exist in two partitions" are told apart.
const partitionsHoldingQuery = "SELECT tableoid::regclass::text FROM %s WHERE id = $1 ORDER BY occurred_at"

// TestASecondRowMayCarryAnAlreadyUsedIdInAnotherPartition asserts the property rather than assuming
// it away. M1 forces the composite key, so an event id is unique by construction alone -- through
// the single identity sequence -- and never by constraint. internal/reconcile and internal/delivery
// map event_queue.event_id to events.id one-to-one; nothing in the catalog enforces that mapping
// for them, so this is the assertion that records the obligation rather than leaving a later
// package to assume a constraint that does not exist.
func TestASecondRowMayCarryAnAlreadyUsedIdInAnotherPartition(t *testing.T) {
	skipIfShort(t)

	pool := migratedSchema(t)
	covered := RequiredRanges(theObservedInstant, theObservedRetention)[:1]
	plantPartitions(t, pool, harnessSchema, covered)
	first := plantEvent(t, pool, covered[0].From)

	// An instant no created range covers, so the second row lands in the permanent DEFAULT
	// partition and the two are demonstrably in different partitions rather than merely both there.
	elsewhere := covered[0].From.AddDate(-1, 0, 0)
	mustExecOn(t, pool, "INSERT INTO "+mustQualify(t, harnessSchema, TableEvents)+
		" (id, "+eventColumns+") OVERRIDING SYSTEM VALUE VALUES ($2, "+eventValues+")",
		elsewhere, first.id)

	holding := rowsHoldingId(t, pool, first.id)
	if len(holding) != 2 {
		t.Fatalf("id %d is held by %d rows %v, want the two written", first.id, len(holding), holding)
	}
	if holding[0] == holding[1] {
		t.Errorf("both rows carrying id %d landed in %s, so this case says nothing about a duplicate "+
			"id *across* partitions, which is the only place one can occur", first.id, holding[0])
	}
}

func rowsHoldingId(t *testing.T, pool *pgxpool.Pool, id int64) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(),
		fmt.Sprintf(partitionsHoldingQuery, mustQualify(t, harnessSchema, TableEvents)), id)
	if err != nil {
		t.Fatalf("read the partitions holding id %d: %v", id, err)
	}
	defer rows.Close()

	holding, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect the partitions holding id %d: %v", id, err)
	}
	return holding
}
