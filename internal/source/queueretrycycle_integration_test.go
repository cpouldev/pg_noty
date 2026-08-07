//go:build integration

package source

import (
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// TestARetriedEventCanBeDeliveredAndRecordedAgain drives the whole operator cycle, which no test
// crossed before: an event that has already produced a deliveries row is retried, claimed and
// settled. RetryBatch resets attempts to 0 -- the specification's own literal -- so the claim that
// follows re-walks an attempt number deliveries already holds, and deliveries is keyed
// (event_id, attempt). recordDelivery inserted with no conflict clause, so the settle transaction
// aborted, the queue row's delete rolled back, and the destination received the event a second time
// on the next claim.
//
// The schema decided this direction already: 0002_objects.sql's comment on response_snippet reasons
// that a rejected insert "would abort the delivery worker's record transaction, leaving the queue row delivering
// until lease reclaim" and concludes that failing closed "is right for enqueue and wrong for
// forensics".
func TestARetriedEventCanBeDeliveredAndRecordedAgain(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	seeded := seedQueueEvent(t, pool, "order_paid")
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "admin", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	// A first delivery that fails permanently, so deliveries holds (event_id, 1).
	claimed, err := source.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("first claim = %+v, %v, want one event", claimed, err)
	}
	if claimed[0].Attempt != 1 {
		t.Fatalf("first claim reported attempt %d, want 1", claimed[0].Attempt)
	}
	if err := source.Dead(t.Context(), claimed[0], Delivery{HTTPStatus: 404}, "gone"); err != nil {
		t.Fatal(err)
	}

	// The operator retries it. attempts returns to 0, so the next claim is attempt 1 again.
	reset, err := source.RetryBatch(t.Context(), QueueSelector{ID: &seeded.ID}, 10)
	if err != nil || reset != 1 {
		t.Fatalf("RetryBatch = %d, %v, want one row reset", reset, err)
	}
	retried, err := source.Claim(t.Context(), 1)
	if err != nil || len(retried) != 1 {
		t.Fatalf("claim after retry = %+v, %v, want one event", retried, err)
	}
	if retried[0].Attempt != 1 {
		t.Fatalf(
			"claim after retry reported attempt %d, want 1 -- RetryBatch resets attempts to 0",
			retried[0].Attempt,
		)
	}

	// The settle must succeed. Before the repair this raised
	// `duplicate key value violates unique constraint "deliveries_pkey"`.
	if err := source.Ack(t.Context(), retried[0], Delivery{HTTPStatus: 200}); err != nil {
		t.Fatalf(
			"Ack after retry failed: %v; the queue row is left delivering and the event is "+
				"redelivered on the next claim", err,
		)
	}
	queue := mustQualifyServiceTable(t, harnessSchema, schema.TableEventQueue)
	var remaining int
	if err := pool.QueryRow(
		t.Context(), "SELECT count(*) FROM "+queue+" WHERE event_id=$1",
		seeded.ID,
	).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Errorf(
			"the queue still holds %d rows for the acked event, so the settle transaction "+
				"rolled back and the event will be delivered again", remaining,
		)
	}
}

// TestRetryingAlreadyPendingEventsTerminates pins the loop bound. RetryBatch's predicate selected on
// status and its assignment set that same status, so with `--status pending` no row ever left the
// selection, RowsAffected never reached 0, and internal/cli's drain loop spun forever -- committing a
// batch per pass and pushing next_attempt_at forward each time, which also starved the events it was
// meant to release.
//
// Both directions are asserted: the first pass must still do the work, and the second must report
// nothing left to do.
func TestRetryingAlreadyPendingEventsTerminates(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	seedQueueEvent(t, pool, "order_paid")
	seedQueueEvent(t, pool, "order_paid")
	queue := mustQualifyServiceTable(t, harnessSchema, schema.TableEventQueue)
	// Back both rows off into the future so the retry has something to pull forward.
	mustExecOn(t, pool, "UPDATE "+queue+" SET attempts=3, next_attempt_at=now()+interval '1 hour'")
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "admin", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	selector := QueueSelector{Listener: "order_paid", Status: "pending"}
	first, err := source.RetryBatch(t.Context(), selector, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if first != 2 {
		t.Fatalf("first retry pass reset %d rows, want 2; the pass must still do its work", first)
	}
	second, err := source.RetryBatch(t.Context(), selector, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Fatalf(
			"second retry pass reset %d rows, want 0; a caller draining until zero never "+
				"terminates while a pass keeps reporting work", second,
		)
	}
}
