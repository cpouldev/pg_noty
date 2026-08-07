//go:build integration

package source

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The lease-ownership clause of the three transition guards, isolated.
//
// Every negative case in TestAckNackDeadAreGuardFirstAndLeaveEventsAlone is a repeat call, which
// the `status='delivering'` clause refuses first -- so `leased_by=$2` decided nothing and could be
// replaced with `$2 = $2` in all three files with both tiers green. What that permits is a second
// worker acknowledging, rescheduling or killing an event the first worker is still holding.
//
// Each case below satisfies both other clauses and differs only in the worker attempting it: the
// row exists under its own event_id and is genuinely `delivering`, leased by the holder. They are
// three named tests rather than a table so a failure names the transition it belongs to, and the
// refusal is asserted together with its consequence -- the holder's lease intact -- rather than on
// its own.

const (
	theHoldingWorker = "worker-a"
	theSecondWorker  = "worker-b"
)

// heldEvent is one event claimed by the holding worker, with the second worker's source ready to
// attempt a transition on it and the queue row as the holder left it.
type heldEvent struct {
	pool   *pgxpool.Pool
	second *TriggerSource
	event  Event
	held   queueRow
}

func anEventHeldByTheFirstWorker(t *testing.T) heldEvent {
	t.Helper()
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	holder := mustOpenWorker(t, pool, theHoldingWorker)
	second := mustOpenWorker(t, pool, theSecondWorker)
	seed := seedQueueEvent(t, pool, "listener")

	claimed, err := holder.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("the holding worker's claim = %#v, %v", claimed, err)
	}
	held := readQueueRow(t, pool, seed.ID)
	if !held.present || held.status != "delivering" || held.leasedUntil == nil ||
		held.leasedBy == nil || *held.leasedBy != theHoldingWorker {
		t.Fatalf("the row is present=%t status=%s leased_by=%v, so the case does not reach the "+
			"ownership clause it is named for", held.present, held.status, held.leasedBy)
	}
	return heldEvent{pool: pool, second: second, event: claimed[0], held: held}
}

func mustOpenWorker(t *testing.T, pool *pgxpool.Pool, leasedBy string) *TriggerSource {
	t.Helper()
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: leasedBy, Lease: time.Minute})
	if err != nil {
		t.Fatalf("open source for %s: %v", leasedBy, err)
	}
	t.Cleanup(func() { _ = source.Close() })
	return source
}

// assertTheHoldersLeaseSurvived is the whole consequence of the refusal: the sentinel, the row
// exactly as the holder left it, and no delivery attributed to a worker that never delivered.
func assertTheHoldersLeaseSurvived(t *testing.T, subject heldEvent, err error) {
	t.Helper()
	if !errors.Is(err, ErrEventNotClaimed) {
		t.Fatalf("the second worker's transition returned %v, want ErrEventNotClaimed: it holds no "+
			"lease on this event", err)
	}
	after := readQueueRow(t, subject.pool, subject.event.ID)
	switch {
	case !after.present:
		t.Fatal("the second worker removed a row it does not hold")
	case after.status != subject.held.status:
		t.Errorf("status = %s, want the holder's %s", after.status, subject.held.status)
	case after.leasedBy == nil || *after.leasedBy != theHoldingWorker:
		t.Errorf("leased_by = %v, want the holder %s", after.leasedBy, theHoldingWorker)
	case after.leasedUntil == nil || !after.leasedUntil.Equal(*subject.held.leasedUntil):
		t.Errorf("leased_until = %v, want the holder's own %v", after.leasedUntil, subject.held.leasedUntil)
	case after.attempts != subject.held.attempts:
		t.Errorf("attempts = %d, want the holder's %d", after.attempts, subject.held.attempts)
	case after.deadReason != nil:
		t.Errorf("dead_reason = %s, and the second worker holds no lease to write one", *after.deadReason)
	}
	if got := deliveryCount(t, subject.pool, subject.event.ID); got != 0 {
		t.Errorf("the refused transition recorded %d deliveries, want 0", got)
	}
}

func aRefusedDelivery() Delivery {
	return Delivery{HTTPStatus: 200, Snippet: "ok", Duration: time.Millisecond}
}

func TestASecondWorkerCannotAckAnEventTheFirstWorkerHolds(t *testing.T) {
	skipIfShort(t)
	subject := anEventHeldByTheFirstWorker(t)
	assertTheHoldersLeaseSurvived(t, subject,
		subject.second.Ack(t.Context(), subject.event, aRefusedDelivery()))
}

func TestASecondWorkerCannotNackAnEventTheFirstWorkerHolds(t *testing.T) {
	skipIfShort(t)
	subject := anEventHeldByTheFirstWorker(t)
	assertTheHoldersLeaseSurvived(t, subject,
		subject.second.Nack(t.Context(), subject.event, aRefusedDelivery(), time.Now().Add(theRetryDelay)))
}

func TestASecondWorkerCannotDeadAnEventTheFirstWorkerHolds(t *testing.T) {
	skipIfShort(t)
	subject := anEventHeldByTheFirstWorker(t)
	assertTheHoldersLeaseSurvived(t, subject,
		subject.second.Dead(t.Context(), subject.event, aRefusedDelivery(), theDeadReason))
}
