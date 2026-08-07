//go:build integration

package source

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// theInFlightDelay is how long a cancellation waits before firing. It only has to outlast the round
// trip that puts the statement into the lock queue, because the lock is held until the call returns.
const theInFlightDelay = 250 * time.Millisecond

// blockTheQueue takes ACCESS EXCLUSIVE on the queue table in a transaction of its own, so every
// statement the four methods issue against it waits. That is what makes "in flight" deterministic:
// the cancellation arrives while the server is executing the statement rather than before it starts
// or after it has committed -- and those two are the states an ordinary race would keep hitting.
func blockTheQueue(t *testing.T, pool *pgxpool.Pool) (release func()) {
	t.Helper()
	holding, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("open the blocking transaction: %v", err)
	}
	mustExecOn(
		t, holding, "LOCK TABLE "+mustQualifyServiceTable(t, harnessSchema, schema.TableEventQueue)+
			" IN ACCESS EXCLUSIVE MODE",
	)
	return func() { _ = holding.Rollback(t.Context()) }
}

// cancelledInFlight runs one call against the blocked queue and cancels it while it waits.
func cancelledInFlight(t *testing.T, call func(context.Context) error) error {
	t.Helper()
	inFlight, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(theInFlightDelay, cancel)
	return call(inFlight)
}

func assertRecognisableCancellation(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"%s cancelled in flight returned %v, which a caller cannot recognise as "+
				"cancellation rather than as a database fault", what, err,
		)
	}
}

// TestCancellingAClaimInFlightTakesEffectForItsWholeBatchOrNone is the cancellation atomicity clause
// for Claim. A claim marks every row of its batch in one statement, so a partial application would
// leave some rows leased to a worker that never received them -- invisible until their leases expire.
func TestCancellingAClaimInFlightTakesEffectForItsWholeBatchOrNone(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	const batch = 5
	seedQueueEvents(t, pool, batch)
	worker := claimableSource(t, pool, "worker-a")

	release := blockTheQueue(t, pool)
	err := cancelledInFlight(
		t, func(ctx context.Context) error {
			_, claimErr := worker.Claim(ctx, batch)
			return claimErr
		},
	)
	release()
	assertRecognisableCancellation(t, "Claim", err)

	if delivering := queueRowsWithStatus(t, pool, "delivering"); delivering != 0 {
		t.Fatalf("%d of the batch are still marked delivering, want none or all %d", delivering, batch)
	}
	if pending := queueRowsWithStatus(t, pool, "pending"); pending != batch {
		t.Fatalf("%d rows are pending after the cancelled claim, want all %d", pending, batch)
	}
	if reclaimed := mustClaim(t, worker, batch); len(reclaimed) != batch {
		t.Fatalf("the batch is no longer claimable: got %d of %d", len(reclaimed), batch)
	}
}

// TestCancellingATransitionInFlightLeavesItDoneOrNotDone is the same clause for the three
// transitions, plus the clause that follows it: the affected event stays claimable once its lease
// expires. It does not become claimable by itself -- reclaimExpiredLeases records why -- so the sweep
// is what this case runs, exactly as the delivery worker has to.
func TestCancellingATransitionInFlightLeavesItDoneOrNotDone(t *testing.T) {
	skipIfShort(t)
	for _, method := range []string{"ack", "nack", "dead"} {
		t.Run(
			method, func(t *testing.T) {
				pool := freshDatabase(t)
				applySourceMigrations(t, pool)
				seed := seedQueueEvent(t, pool, "listener")
				worker := claimableSource(t, pool, "worker-a")
				claimed := mustClaim(t, worker, 1)
				if len(claimed) != 1 {
					t.Fatalf("claim returned %d events, want one to transition", len(claimed))
				}
				before := readEventSnapshot(t, pool, seed.ID)

				release := blockTheQueue(t, pool)
				err := cancelledInFlight(
					t, func(ctx context.Context) error {
						return callTransition(worker, ctx, method, claimed[0])
					},
				)
				release()
				assertRecognisableCancellation(t, method, err)

				// Not done, and wholly not done: the guard runs first, so a transition that had
				// half-applied would show a deliveries row for a queue row it never moved.
				if got := queueRowsWithStatus(t, pool, "delivering"); got != 1 {
					t.Fatalf(
						"the queue holds %d delivering rows after a cancelled %s, want the one "+
							"the claim left", got, method,
					)
				}
				if got := deliveryCount(t, pool, seed.ID); got != 0 {
					t.Fatalf("a cancelled %s recorded %d deliveries, want none", method, got)
				}
				assertEventUnchanged(t, before, readEventSnapshot(t, pool, seed.ID))

				expireTheLease(t, pool, seed.ID)
				if swept := reclaimExpiredLeases(t, pool); swept != 1 {
					t.Fatalf(
						"the sweep reset %d rows after a cancelled %s, want the one stranded row",
						swept, method,
					)
				}
				if recovered := mustClaim(t, worker, 1); len(recovered) != 1 || recovered[0].ID != seed.ID {
					t.Fatalf(
						"the event affected by a cancelled %s is not claimable again: %d events",
						method, len(recovered),
					)
				}
			},
		)
	}
}

func callTransition(source *TriggerSource, ctx context.Context, method string, event Event) error {
	switch method {
	case "ack":
		return source.Ack(ctx, event, Delivery{})
	case "nack":
		return source.Nack(ctx, event, Delivery{}, time.Now().Add(time.Second))
	default:
		return source.Dead(ctx, event, Delivery{}, "cancelled")
	}
}

func queueRowsWithStatus(t *testing.T, pool *pgxpool.Pool, status string) int {
	t.Helper()
	return countOn(
		t, pool, "SELECT count(*) FROM "+
			mustQualifyServiceTable(t, harnessSchema, schema.TableEventQueue)+" WHERE status=$1", status,
	)
}

func assertEventUnchanged(t *testing.T, before, after Event) {
	t.Helper()
	if before.ID != after.ID || !before.OccurredAt.Equal(after.OccurredAt) || before.Listener != after.Listener ||
		before.Operation != after.Operation || before.Table != after.Table || before.TXID != after.TXID ||
		string(before.Payload) != string(after.Payload) {
		t.Error("a cancelled transition modified the append-only event")
	}
}
