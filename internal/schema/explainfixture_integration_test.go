//go:build integration

package schema

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The fixture criteria 8 and 9 rest on, and the two guards that keep it from being a fixture in name
// only: the queue is populated past the measured sequential-scan threshold, and ANALYZE has run
// before any plan is taken. explain_integration_test.go holds the assertions themselves.

const (
	theQueueTable = "event_queue"
	theClaimIndex = "event_queue_claim_idx"
	// theClaimOrderingColumn leads the claim index and carries the range condition;
	// theClaimTieBreakColumn follows it, breaks ties deterministically and is what keeps the claim
	// ordering satisfied from the index, so no Sort node appears.
	theClaimOrderingColumn = "next_attempt_at"
	theClaimTieBreakColumn = "event_id"

	// theQueuedEventCount is how many rows the queue holds before any plan is taken. Measured on
	// PostgreSQL 17.10 against this exact schema, walking the population up: at 50 rows the inner
	// scan is a Seq Scan beneath a Sort; from ~200 rows the claim index is chosen for it, but the
	// *outer* side of the UPDATE keeps a Seq Scan on event_queue beneath a Hash Join; from ~5000
	// rows the outer side becomes a Nested Loop over the primary key and no sequential scan remains.
	// 20000 is four times past the last of those thresholds, which is what "well past the point
	// where a sequential scan would otherwise be chosen" has to mean for a plan asserted to hold no
	// sequential scan at all.
	theQueuedEventCount = 20000
	// theDeadEveryNthRow keeps the claim index partial on something: one queue row in ten is dead,
	// so the index covers a proper subset of the table rather than all of it.
	theDeadEveryNthRow = 10

	theClaimBatch    = 10
	theClaimListener = "orders"
	theClaimTarget   = "public.orders"
	theClaimWorker   = "pgnoty-worker-1"
	theClaimLease    = 30 * time.Second
)

// theClaimArguments binds $1, $2 and $3 at EXPLAIN time, as values of the types production binds:
// text for leased_by, interval for the lease, int for the LIMIT. Binding changes the parameters and
// never the query text, which is what keeps criterion 8's "verbatim" true of what is planned.
func theClaimArguments() []any {
	return []any{
		theClaimWorker,
		pgtype.Interval{Microseconds: theClaimLease.Microseconds(), Valid: true},
		int32(theClaimBatch),
	}
}

// theClaimQuery is the fixture's text, with the one assumption it rests on stated out loud:
// internal/delivery writes `noty.event_queue` schema-qualified, and the harness serves that same
// schema, which is why this text runs here at all. A harness renamed elsewhere fails with this
// sentence rather than with a relation-does-not-exist error nobody would read as a fixture
// mismatch.
func theClaimQuery(t *testing.T) string {
	t.Helper()

	query := claimQueryText(t)
	if named := harnessSchema + "." + theQueueTable; !strings.Contains(query, named) {
		t.Fatalf("%s plans against %s and the harness serves schema %q; the two have to name the "+
			"same queue:\n%s", theClaimQueryFixture, named, harnessSchema, query)
	}
	return query
}

// aPopulatedQueue is the fixture both plan assertions rest on: the shipped corpus applied by the
// real runner, a queue populated past the sequential-scan threshold, and ANALYZE run before any plan
// is taken. Without the last two the planner chooses a sequential scan whatever indexes exist, and
// an assertion taken there would prove nothing about production (skill Pattern 10).
func aPopulatedQueue(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool := migratedSchema(t)
	events, queue := mustQualify(t, harnessSchema, "events"), mustQualify(t, harnessSchema, theQueueTable)

	mustExecOn(t, pool, "INSERT INTO "+events+
		" (listener, table_name, operation, payload, txid, occurred_at)"+
		" SELECT $1, $2, 'INSERT', jsonb_build_object('n', g), pg_current_xact_id(),"+
		" now() - (g || ' seconds')::interval FROM generate_series(1, $3) AS g",
		theClaimListener, theClaimTarget, theQueuedEventCount)
	mustExecOn(t, pool, "INSERT INTO "+queue+
		" (event_id, occurred_at, listener, status, attempts, next_attempt_at)"+
		" SELECT id, occurred_at, listener,"+
		" CASE WHEN id % $1 = 0 THEN 'dead' ELSE 'pending' END, 0, occurred_at FROM "+events,
		theDeadEveryNthRow)
	mustExecOn(t, pool, "ANALYZE "+queue)

	assertPopulatedAndAnalysed(t, pool, queue)
	return pool
}

// assertPopulatedAndAnalysed is CK-1's vacuity guard, and it reads the two facts separately because
// they fail differently. A queue short of its population plans as a small table; a queue never
// analysed carries reltuples = -1, and the planner then works from a default estimate rather than
// from this fixture -- in both cases every plan assertion after it would be about nothing.
func assertPopulatedAndAnalysed(t *testing.T, pool *pgxpool.Pool, queue string) {
	t.Helper()

	var written int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+queue).Scan(&written); err != nil {
		t.Fatalf("count the rows in %s: %v", queue, err)
	}
	if written != theQueuedEventCount {
		t.Fatalf("%s holds %d rows, want %d: below the measured threshold the planner chooses a "+
			"sequential scan whatever indexes exist", queue, written, theQueuedEventCount)
	}

	var estimated float64
	if err := pool.QueryRow(t.Context(),
		"SELECT reltuples FROM pg_catalog.pg_class WHERE oid = to_regclass($1)", queue).
		Scan(&estimated); err != nil {
		t.Fatalf("read the statistics on %s: %v", queue, err)
	}
	// ANALYZE samples 30000 rows by default and this fixture is smaller, so it reads every row and
	// the estimate is the count.
	if int(estimated) != written {
		t.Fatalf("%s holds %d rows and its statistics say %v; -1 is the never-analysed sentinel",
			queue, written, estimated)
	}
}
