//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTheQueueIsReadOnlyAsABoundedAggregate(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	for _, row := range []struct{ listener, status string }{
		{"orders", "pending"}, {"orders", "pending"}, {"orders", "delivering"}, {"orders", "dead"},
	} {
		seedQueueStatus(t, pool, row.listener, row.status)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	counts, err := readQueueCounts(t.Context(), tx, harnessSchema, "orders")
	if err != nil {
		t.Fatal(err)
	}
	// 2 pending + 1 delivering = 3 live; 1 dead remains separately reported.
	if counts.Live != 3 || counts.Dead != 1 || counts.Pending != 2 || counts.Delivering != 1 {
		t.Fatalf("queue counts = %+v, want pending=2 delivering=1 live=3 dead=1", counts)
	}
	empty, err := readQueueCounts(t.Context(), tx, harnessSchema, "empty")
	if err != nil || empty.Live != 0 || empty.Dead != 0 {
		t.Fatalf("empty queue counts = %+v, %v; want zero", empty, err)
	}
}

func seedQueueStatus(t *testing.T, pool *pgxpool.Pool, listener, status string) {
	t.Helper()
	events, _ := schema.Qualified(harnessSchema, schema.TableEvents)
	queue, _ := schema.Qualified(harnessSchema, schema.TableEventQueue)
	var id int64
	if err := pool.QueryRow(
		t.Context(),
		"INSERT INTO "+events+" (listener, table_name, operation, payload, txid, occurred_at) VALUES ($1, $2, 'insert', '{}', pg_current_xact_id(), clock_timestamp()) RETURNING id",
		listener,
		"\"public\".\"orders\"",
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	mustExecOn(
		t,
		pool,
		"INSERT INTO "+queue+" (event_id, occurred_at, listener, status, attempts, next_attempt_at) SELECT id, occurred_at, listener, $2, 0, occurred_at FROM "+events+" WHERE id=$1",
		id,
		status,
	)
}
