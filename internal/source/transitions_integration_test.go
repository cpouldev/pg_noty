//go:build integration

package source

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func readEventSnapshot(t *testing.T, pool *pgxpool.Pool, id int64) Event {
	t.Helper()
	var event Event
	err := pool.QueryRow(t.Context(), "SELECT id, occurred_at, listener, operation, table_name, txid, payload FROM noty.events WHERE id=$1", id).Scan(&event.ID, &event.OccurredAt, &event.Listener, &event.Operation, &event.Table, &event.TXID, &event.Payload)
	if err != nil {
		t.Fatalf("read event %d: %v", id, err)
	}
	return event
}

func deliveryCount(t *testing.T, pool *pgxpool.Pool, id int64) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM noty.deliveries WHERE event_id=$1", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestAckNackDeadAreGuardFirstAndLeaveEventsAlone(t *testing.T) {
	skipIfShort(t)
	for _, method := range []string{"ack", "nack", "dead"} {
		t.Run(method, func(t *testing.T) {
			pool := freshDatabase(t)
			applySourceMigrations(t, pool)
			source, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "worker", Lease: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			seed := seedQueueEvent(t, pool, "listener")
			claimed, err := source.Claim(t.Context(), 1)
			if err != nil || len(claimed) != 1 {
				t.Fatalf("claim = %#v, %v", claimed, err)
			}
			before := readEventSnapshot(t, pool, seed.ID)
			delivery := Delivery{HTTPStatus: 200, Snippet: "ok", Duration: time.Millisecond}
			switch method {
			case "ack":
				err = source.Ack(t.Context(), claimed[0], delivery)
			case "nack":
				err = source.Nack(t.Context(), claimed[0], delivery, time.Now().Add(time.Second))
			case "dead":
				err = source.Dead(t.Context(), claimed[0], delivery, "permanent")
			}
			if err != nil {
				t.Fatal(err)
			}
			after := readEventSnapshot(t, pool, seed.ID)
			if before.ID != after.ID || !before.OccurredAt.Equal(after.OccurredAt) || before.Listener != after.Listener || before.Operation != after.Operation || before.Table != after.Table || before.TXID != after.TXID || string(before.Payload) != string(after.Payload) {
				t.Fatal("transition modified the append-only event")
			}
			if deliveryCount(t, pool, seed.ID) != 1 {
				t.Fatal("successful transition did not record exactly one delivery")
			}
			if err := repeatTransition(t, source, method, claimed[0], delivery); !errors.Is(err, ErrEventNotClaimed) {
				t.Fatalf("guard path error = %v, want ErrEventNotClaimed", err)
			}
			if deliveryCount(t, pool, seed.ID) != 1 {
				t.Fatal("guard path inserted a delivery")
			}
		})
	}
}

func repeatTransition(t *testing.T, source *TriggerSource, method string, event Event, delivery Delivery) error {
	t.Helper()
	switch method {
	case "ack":
		return source.Ack(t.Context(), event, delivery)
	case "nack":
		return source.Nack(t.Context(), event, delivery, time.Now().Add(time.Second))
	default:
		return source.Dead(t.Context(), event, delivery, "late")
	}
}

func TestCancelledContextsAreRecognisable(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "worker", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	event := Event{ID: 999, Attempt: 1}
	delivery := Delivery{}
	cases := []struct {
		name string
		call func(context.Context) error
	}{
		{"claim", func(ctx context.Context) error { _, err := source.Claim(ctx, 1); return err }},
		{"ack", func(ctx context.Context) error { return source.Ack(ctx, event, delivery) }},
		{"nack", func(ctx context.Context) error { return source.Nack(ctx, event, delivery, time.Now()) }},
		{"dead", func(ctx context.Context) error { return source.Dead(ctx, event, delivery, "cancelled") }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.call(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want cancellation", err)
			}
		})
	}
}

func TestNackUsesServerEpochWithSkewedWorkerClock(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "worker", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	seed := seedQueueEvent(t, pool, "listener")
	claimed, err := source.Claim(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	source.now = func() time.Time { return time.Unix(0, 0) }
	retryAt := time.Unix(5, 0)
	if err := source.Nack(t.Context(), claimed[0], Delivery{}, retryAt); err != nil {
		t.Fatal(err)
	}
	var next time.Time
	if err := pool.QueryRow(t.Context(), "SELECT next_attempt_at FROM noty.event_queue WHERE event_id=$1", seed.ID).Scan(&next); err != nil {
		t.Fatal(err)
	}
	var serverNow time.Time
	if err := pool.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&serverNow); err != nil {
		t.Fatal(err)
	}
	if delta := next.Sub(serverNow); delta < 4*time.Second || delta > 6*time.Second {
		t.Fatalf("server retry delay = %s, want about 5s", delta)
	}
}
