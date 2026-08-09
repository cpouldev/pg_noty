//go:build integration

package source

import (
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestQueueAdminObserveListAndRetry(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	first := seedQueueEvent(t, pool, "order_paid")
	second := seedQueueEvent(t, pool, "order_paid")
	third := seedQueueEvent(t, pool, "refund")
	queue := mustQualifyServiceTable(t, harnessSchema, schema.TableEventQueue)
	mustExecOn(
		t,
		pool,
		"UPDATE "+queue+" SET status='dead', attempts=4, dead_reason='gone' WHERE event_id=$1",
		second.ID,
	)
	mustExecOn(t, pool, "UPDATE "+queue+" SET status='delivering', attempts=2 WHERE event_id=$1", third.ID)
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "admin", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	observed, err := source.ObserveQueue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if observed.Pending != 1 || observed.Delivering != 1 || observed.Dead != 1 || observed.DeadByListener["order_paid"] != 1 {
		t.Fatalf("queue observation = %+v, want one row in each status and one dead order_paid", observed)
	}
	all, err := source.ListEvents(t.Context(), QueueSelector{})
	if err != nil || len(all) != 3 {
		t.Fatalf("unfiltered events = %d, %v, want 3", len(all), err)
	}
	dead, err := source.ListEvents(t.Context(), QueueSelector{Status: "dead"})
	if err != nil || len(dead) != 1 || dead[0].ID != second.ID {
		t.Fatalf("dead events = %+v, %v", dead, err)
	}
	listener, err := source.ListEvents(t.Context(), QueueSelector{Listener: "order_paid"})
	if err != nil || len(listener) != 2 {
		t.Fatalf("listener events = %d, %v, want 2", len(listener), err)
	}
	combined, err := source.ListEvents(t.Context(), QueueSelector{Listener: "order_paid", Status: "dead"})
	if err != nil || len(combined) != 1 || combined[0].ID != second.ID {
		t.Fatalf("combined events = %+v, %v", combined, err)
	}
	if _, err := source.RetryBatch(t.Context(), QueueSelector{}, 10); err == nil {
		t.Fatal("empty retry selector was accepted")
	}
	if _, err := source.RetryBatch(t.Context(), QueueSelector{ID: &first.ID, Status: "dead"}, 10); err == nil {
		t.Fatal("conflicting retry selector was accepted")
	}
	reset, err := source.RetryBatch(t.Context(), QueueSelector{Listener: "order_paid", Status: "dead"}, 1)
	if err != nil || reset != 1 {
		t.Fatalf("retry reset = %d, %v, want one", reset, err)
	}
	row := readQueueRow(t, pool, second.ID)
	if row.status != "pending" {
		t.Errorf("status = %s, want pending", row.status)
	}
	if row.attempts != 0 {
		t.Errorf("attempts = %d, want zero", row.attempts)
	}
	if row.deadReason != nil {
		t.Errorf("dead_reason = %v, want nil", *row.deadReason)
	}
	if row.nextAttempt.Before(serverNow(t, pool).Add(-time.Second)) {
		t.Error("retry scheduled before the server's current time")
	}
	// Both order_paid rows are now pending, due, at zero attempts and carrying no dead_reason -- the
	// state a retry produces -- so a retry of them changes nothing and must report nothing. This row
	// asserted one reset, which is the behaviour that made the drain loop non-terminating: the
	// predicate selected on status while the assignment set that same status, so no row ever left
	// the selection and `events retry --status pending` never reached the zero the specification's
	// loop stops on. TestRetryingAlreadyPendingEventsTerminates covers the case where there *is*
	// work -- pending rows backed off into the future -- so the selector is still proven to act.
	if reset, err := source.RetryBatch(
		t.Context(),
		QueueSelector{Listener: "order_paid", Status: "pending"},
		1,
	); err != nil || reset != 0 {
		t.Fatalf(
			"equality retry reset = %d, %v, want zero; every order_paid row is already in the "+
				"state a retry produces", reset, err,
		)
	}
}
