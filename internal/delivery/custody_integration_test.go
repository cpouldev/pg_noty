//go:build integration

package delivery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var deliverySchemaSerial atomic.Uint64

type deliveryDB struct {
	pool *pgxpool.Pool
	cfg  config.Config
}

func newDeliveryDatabase(t *testing.T) deliveryDB {
	t.Helper()
	pool := deliveryPool(t)
	name := fmt.Sprintf("d_%d_%d", deliverySchemaSerial.Add(1), time.Now().UnixNano())
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	quoted, fault := schema.Quoted(name)
	if fault != schema.IdentifierOK {
		conn.Release()
		t.Fatal(fault)
	}
	if _, err := conn.Exec(t.Context(), "CREATE SCHEMA "+quoted); err != nil {
		conn.Release()
		t.Fatal(err)
	}
	if _, err := conn.Exec(t.Context(), "SET search_path TO "+quoted+", public"); err != nil {
		conn.Release()
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join("..", "schema", "migrations", "*.sql"))
	if err != nil || len(paths) == 0 {
		conn.Release()
		t.Fatalf("read migrations: %v", err)
	}
	sort.Strings(paths)
	for _, path := range paths {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			conn.Release()
			t.Fatal(readErr)
		}
		if _, execErr := conn.Exec(t.Context(), string(body)); execErr != nil {
			conn.Release()
			t.Fatalf("apply %s: %v", path, execErr)
		}
	}
	_, _ = conn.Exec(t.Context(), "SET search_path TO DEFAULT")
	conn.Release()
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE") })
	return deliveryDB{
		pool: pool, cfg: config.Config{
			Version: 1, Instance: name,
			Database: config.Database{URL: pool.Config().ConnConfig.ConnString(), Schema: name},
		},
	}
}

func deliveryTable(t *testing.T, db deliveryDB, table string) string {
	t.Helper()
	qualified, fault := schema.Qualified(db.cfg.Database.Schema, table)
	if fault != schema.IdentifierOK {
		t.Fatal(fault)
	}
	return qualified
}

func seedDeliveryEvent(t *testing.T, db deliveryDB, listener string, payload string) source.Event {
	t.Helper()
	events, queue := deliveryTable(t, db, schema.TableEvents), deliveryTable(t, db, schema.TableEventQueue)
	var event source.Event
	err := db.pool.QueryRow(
		t.Context(),
		"INSERT INTO "+events+
			" (listener, table_name, operation, payload, txid, occurred_at) VALUES ($1, $2, $3, $4, pg_current_xact_id(), clock_timestamp()) RETURNING id, occurred_at, txid",
		listener,
		`"public"."orders"`,
		"insert",
		payload,
	).
		Scan(&event.ID, &event.OccurredAt, &event.TXID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(
		t.Context(),
		"INSERT INTO "+queue+
			" (event_id, occurred_at, listener, status, attempts, next_attempt_at) VALUES ($1, $2, $3, 'pending', 0, clock_timestamp())",
		event.ID,
		event.OccurredAt,
		listener,
	); err != nil {
		t.Fatal(err)
	}
	event.Listener, event.Operation, event.Table, event.Payload = listener, "insert", `"public"."orders"`, []byte(payload)
	return event
}

func queueState(t *testing.T, db deliveryDB, id int64) (
	present bool,
	status string,
	attempts int,
	leasedUntil *time.Time,
	leasedBy *string,
) {
	t.Helper()
	queue := deliveryTable(t, db, schema.TableEventQueue)
	err := db.pool.QueryRow(
		t.Context(),
		"SELECT status, attempts, leased_until, leased_by FROM "+queue+" WHERE event_id=$1",
		id,
	).
		Scan(&status, &attempts, &leasedUntil, &leasedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", 0, nil, nil
		}
		t.Fatal(err)
	}
	return true, status, attempts, leasedUntil, leasedBy
}

func TestAtLeastOnceDuplicateAfterLeaseExpiryHasIdenticalBodies(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":2}`)
	first := openDeliverySource(t, db, "worker-a", 100*time.Millisecond)
	claimed, err := first.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	hold := make(chan struct{})
	fixture := newHTTPFixture(t, HTTPReply{Status: 200, Hold: hold}, HTTPReply{Status: 200})
	workerA := NewWorker(
		first,
		first,
		WorkerConfig{Listeners: []ListenerConfig{deliveryListener("orders", fixture.URL(), Policy{MaxAttempts: 2})}},
	)
	if !workerA.Dispatch(t.Context(), claimed[0]) {
		t.Fatal("first delivery was not admitted")
	}
	for attempt := 0; attempt < 40 && len(fixture.Requests()) < 1; attempt++ {
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := db.pool.Exec(
		t.Context(),
		"UPDATE "+deliveryTable(
			t,
			db,
			schema.TableEventQueue,
		)+" SET leased_until=now()-interval '1 second' WHERE event_id=$1",
		event.ID,
	); err != nil {
		t.Fatal(err)
	}
	second := openDeliverySource(t, db, "worker-b", time.Second)
	if swept, err := second.ReclaimExpired(t.Context()); err != nil || swept != 1 {
		t.Fatalf("reclaim = %d, %v", swept, err)
	}
	reclaimed, err := second.Claim(t.Context(), 1)
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("second claim = %#v, %v", reclaimed, err)
	}
	workerB := NewWorker(
		second,
		second,
		WorkerConfig{Listeners: []ListenerConfig{deliveryListener("orders", fixture.URL(), Policy{MaxAttempts: 2})}},
	)
	if !workerB.Dispatch(t.Context(), reclaimed[0]) {
		t.Fatal("replacement delivery was not admitted")
	}
	for attempt := 0; attempt < 40 && len(fixture.Requests()) < 2; attempt++ {
		time.Sleep(5 * time.Millisecond)
	}
	close(hold)
	workerA.Wait()
	workerB.Wait()
	requests := fixture.Requests()
	if len(requests) != 2 || string(requests[0].Body) != string(requests[1].Body) ||
		requests[0].Header.Get("X-Pg-Noty-Event-Id") != requests[1].Header.Get("X-Pg-Noty-Event-Id") {
		t.Fatalf("duplicate requests = %#v, want same id and byte-identical bodies", requests)
	}
	if present, status, _, _, _ := queueState(t, db, event.ID); present || status != "" {
		t.Fatalf("duplicate ended with queue present=%t status=%q, want delivered", present, status)
	}
	if got := deliveryCount(t, db, event.ID); got != 1 || reclaimed[0].Attempt != 2 {
		t.Fatalf("duplicate custody deliveries=%d final attempt=%d, want 1/2", got, reclaimed[0].Attempt)
	}
}

func TestReconciliationFindsEveryEventDeliveredOrExplicitlyDead(t *testing.T) {
	db := newDeliveryDatabase(t)
	success := seedDeliveryEvent(t, db, "orders", `{"id":17}`)
	terminal := seedDeliveryEvent(t, db, "orders", `{"id":18}`)
	oversized := seedDeliveryEvent(t, db, "orders", `{"id": "this payload is over the limit"}`)
	src := openDeliverySource(t, db, "worker", time.Minute)
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK}, HTTPReply{Status: http.StatusBadRequest})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			BatchSize: 3, GlobalConcurrency: 3, Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), MaxPayloadBytes: 12, Client: http.DefaultClient,
					Policy: Policy{MaxAttempts: 2},
				},
			},
		},
	)
	for cycle := 0; cycle < 3; cycle++ {
		if _, err := worker.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		worker.Wait()
	}
	dead, delivered := 0, 0
	for _, event := range []struct{ id int64 }{{success.ID}, {terminal.ID}, {oversized.ID}} {
		present, status, _, _, _ := queueState(t, db, event.id)
		if present {
			var reason *string
			query := "SELECT dead_reason FROM " + deliveryTable(t, db, schema.TableEventQueue) + " WHERE event_id=$1"
			if !present || status != "dead" || db.pool.QueryRow(
				t.Context(),
				query,
				event.id,
			).Scan(&reason) != nil || reason == nil || *reason == "" {
				t.Fatalf(
					"event %d reconciliation state present=%t status=%q reason=%v",
					event.id,
					present,
					status,
					reason,
				)
			}
			dead++
		} else {
			delivered++
		}
	}
	if dead == 0 || delivered == 0 {
		t.Fatalf("reconciliation observed dead=%d delivered=%d, want both outcomes", dead, delivered)
	}
}
