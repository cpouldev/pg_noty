//go:build integration

package source

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedQueueEvent(t *testing.T, pool *pgxpool.Pool, listener string) Event {
	t.Helper()
	return seedQueueEventIn(t, pool, harnessSchema, listener)
}

// seedQueueEventIn writes one event and its pending queue row into one service schema. The schema
// is a parameter rather than a literal because serviceschema_integration_test.go needs two
// populated service schemas at once; every caller wanting the harness's own goes through
// seedQueueEvent.
func seedQueueEventIn(t *testing.T, pool *pgxpool.Pool, serviceSchema, listener string) Event {
	t.Helper()
	events, queue := mustQualifyServiceTable(t, serviceSchema, schema.TableEvents),
		mustQualifyServiceTable(t, serviceSchema, schema.TableEventQueue)
	var event Event
	err := pool.QueryRow(
		t.Context(),
		"INSERT INTO "+events+" (listener, table_name, operation, payload, txid, occurred_at) VALUES ($1, $2, $3, $4, pg_current_xact_id(), clock_timestamp()) RETURNING id, occurred_at",
		listener,
		`"public"."orders"`,
		"insert",
		`{"id":1}`,
	).Scan(&event.ID, &event.OccurredAt)
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
	mustExecOn(
		t,
		pool,
		"INSERT INTO "+queue+" (event_id, occurred_at, listener, status, attempts, next_attempt_at) VALUES ($1, $2, $3, 'pending', 0, clock_timestamp())",
		event.ID,
		event.OccurredAt,
		listener,
	)
	event.Listener, event.Operation, event.Table, event.Payload = listener, "insert", `"public"."orders"`, []byte(`{"id":1}`)
	return event
}

func mustQualifyServiceTable(t *testing.T, serviceSchema, table string) string {
	t.Helper()
	qualified, err := qualifiedServiceTable(serviceSchema, table)
	if err != nil {
		t.Fatalf("qualify %s.%s: %v", serviceSchema, table, err)
	}
	return qualified
}

func applySourceMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	applySourceMigrationsInto(t, pool, harnessSchema)
}

// applySourceMigrationsInto installs internal/schema's corpus into one service schema. The corpus
// names no object qualified -- migration 0001's header records why -- so the schema is chosen by
// search_path, which is why it is set on a connection of its own and returned to the session
// default before release rather than left on a pooled connection for whatever runs next.
func applySourceMigrationsInto(t *testing.T, pool *pgxpool.Pool, serviceSchema string) {
	t.Helper()
	quoted, fault := schema.Quoted(serviceSchema)
	if fault != schema.IdentifierOK {
		t.Fatalf("service schema %s is unusable: %s", serviceSchema, fault)
	}
	connection, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	mustExecOn(t, connection, "CREATE SCHEMA IF NOT EXISTS "+quoted)
	mustExecOn(t, connection, "SET search_path TO "+quoted+", public")
	defer mustExecOn(t, connection, "SET search_path TO DEFAULT")
	paths, err := filepath.Glob(filepath.Join("..", "schema", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		mustExecOn(t, connection, string(data))
	}
}

func seedQueueEvents(t *testing.T, pool *pgxpool.Pool, count int) []Event {
	events := make([]Event, 0, count)
	for index := 0; index < count; index++ {
		events = append(events, seedQueueEvent(t, pool, "listener_"+string(rune('a'+index%20))))
	}
	return events
}

func TestClaimBatchBoundAndSurplusRemainsClaimable(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	seedQueueEvents(t, pool, 6)
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "worker-a", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	claimed, err := source.Claim(t.Context(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 5 {
		t.Fatalf("claimed %d rows, want 5", len(claimed))
	}
	for _, event := range claimed {
		if event.Attempt != 1 {
			t.Errorf("event %d attempt = %d, want 1", event.ID, event.Attempt)
		}
	}
	remaining, err := source.Claim(t.Context(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Fatalf("surplus claim returned %d rows, want 1", len(remaining))
	}
}

func TestConcurrentClaimersHaveAnEmptyIntersection(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	seedQueueEvents(t, pool, 2000)
	first, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "worker-a", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "worker-b", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	type result struct {
		events []Event
		err    error
	}
	results := make(chan result, 2)
	go func() { events, err := first.Claim(t.Context(), 2000); results <- result{events, err} }()
	go func() { events, err := second.Claim(t.Context(), 2000); results <- result{events, err} }()
	a, b := <-results, <-results
	if a.err != nil || b.err != nil {
		t.Fatalf("concurrent claim errors: %v, %v", a.err, b.err)
	}
	ids := make(map[int64]bool, len(a.events))
	for _, event := range a.events {
		ids[event.ID] = true
	}
	for _, event := range b.events {
		if ids[event.ID] {
			t.Fatalf("event %d appeared in both claim results", event.ID)
		}
	}
	if len(a.events)+len(b.events) != 2000 {
		t.Fatalf("union has %d events, want 2000", len(a.events)+len(b.events))
	}
}

func TestClaimWithNoEligibleRowsReturnsEmptyNil(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "worker", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	got, err := source.Claim(t.Context(), 1)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty claim = %#v, %v; want empty nil", got, err)
	}
}
