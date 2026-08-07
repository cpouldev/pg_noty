//go:build integration

package source

import (
	"errors"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The three transition outcomes, read back off the queue row.
//
// Nothing in the repository read event_queue.status after a transition, and `dead_reason` appeared
// in no assertion at all, so a Dead implemented as DELETE passed the whole suite: the repeat-call
// guard still answered ErrEventNotClaimed, the events snapshot was still unchanged, and the
// delivery count was still one. Each case below asserts what the contract actually claims about
// the row that is left behind.

const (
	// theDeadReason is distinctive so an assertion cannot pass against a reason the row already
	// carried or against an empty column.
	theDeadReason = "endpoint retired: no further attempts"
	// theRetryDelay is the distance between the worker's clock and the instant handed to Nack. It
	// is large enough that a Nack dropping the delay -- scheduling now() instead of the supplied
	// instant -- lands outside the bound below.
	theRetryDelay = 5 * time.Second
	// theRetryTolerance is the slack between reading the server clock here and the server reading
	// its own inside the transition.
	theRetryTolerance = time.Second
)

// queueRow is one delivery queue row as the transitions leave it. The three nullable columns are
// pointers because "cleared" and "still held" are exactly what separates a completed transition
// from one whose guard refused.
type queueRow struct {
	present     bool
	status      string
	attempts    int
	nextAttempt time.Time
	leasedUntil *time.Time
	leasedBy    *string
	deadReason  *string
}

// readQueueRow is this package's one reader of a queue row's own state. The transition outcomes
// below, leaseowner_integration_test.go's ownership cases and serviceschema_integration_test.go's
// two populated schemas all ask it, so "the row is untouched", "the row is dead with a reason" and
// "the row in the other schema was never touched" cannot come to disagree about what they read.
func readQueueRow(t *testing.T, pool *pgxpool.Pool, id int64) queueRow {
	t.Helper()
	return readQueueRowIn(t, pool, harnessSchema, id)
}

func readQueueRowIn(t *testing.T, pool *pgxpool.Pool, serviceSchema string, id int64) queueRow {
	t.Helper()
	var row queueRow
	queue := mustQualifyServiceTable(t, serviceSchema, schema.TableEventQueue)
	err := pool.QueryRow(
		t.Context(),
		"SELECT status, attempts, next_attempt_at, leased_until, leased_by, dead_reason "+
			"FROM "+queue+" WHERE event_id=$1", id,
	).
		Scan(&row.status, &row.attempts, &row.nextAttempt, &row.leasedUntil, &row.leasedBy, &row.deadReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return queueRow{}
	}
	if err != nil {
		t.Fatalf("read queue row %d in %s: %v", id, serviceSchema, err)
	}
	row.present = true
	return row
}

func serverNow(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	var now time.Time
	if err := pool.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	return now
}

func assertLeaseCleared(t *testing.T, row queueRow) {
	t.Helper()
	if row.leasedBy != nil {
		t.Errorf("leased_by = %s after the transition, want it cleared", *row.leasedBy)
	}
	if row.leasedUntil != nil {
		t.Errorf("leased_until = %s after the transition, want it cleared", *row.leasedUntil)
	}
}

var queueOutcomeCases = []struct {
	name   string
	apply  func(t *testing.T, source *TriggerSource, claimed Event) error
	assert func(t *testing.T, row queueRow, before time.Time)
}{
	{
		name: "ack removes the row",
		apply: func(t *testing.T, source *TriggerSource, claimed Event) error {
			return source.Ack(t.Context(), claimed, Delivery{HTTPStatus: 200, Duration: time.Millisecond})
		},
		assert: func(t *testing.T, row queueRow, _ time.Time) {
			if row.present {
				t.Fatalf("Ack left the queue row behind with status %s, and Ack deletes it", row.status)
			}
		},
	},
	{
		name: "nack returns the row to pending with the supplied instant",
		apply: func(t *testing.T, source *TriggerSource, claimed Event) error {
			return source.Nack(
				t.Context(), claimed, Delivery{Err: "refused", Duration: time.Millisecond},
				source.now().Add(theRetryDelay),
			)
		},
		assert: func(t *testing.T, row queueRow, before time.Time) {
			if !row.present || row.status != "pending" {
				t.Fatalf("Nack left present=%t status=%s, want a retained row in pending", row.present, row.status)
			}
			// now() inside the transition is at or after the reading taken before the call, so the
			// supplied five seconds cannot measure shorter than that here. The server-versus-worker
			// half of the arithmetic is TestNackUsesServerEpochWithSkewedWorkerClock's.
			if scheduled := row.nextAttempt.Sub(before); scheduled < theRetryDelay-theRetryTolerance {
				t.Errorf(
					"next_attempt_at is %s after the pre-call server clock, want about the "+
						"supplied %s: a Nack scheduling now() rather than the instant it was given "+
						"redelivers immediately", scheduled, theRetryDelay,
				)
			}
			assertLeaseCleared(t, row)
		},
	},
	{
		name: "dead retains the row with its reason",
		apply: func(t *testing.T, source *TriggerSource, claimed Event) error {
			return source.Dead(t.Context(), claimed, Delivery{Err: "gone", Duration: time.Millisecond}, theDeadReason)
		},
		assert: func(t *testing.T, row queueRow, _ time.Time) {
			if !row.present {
				t.Fatal("Dead removed the queue row, and Dead retains it for retention")
			}
			if row.status != "dead" {
				t.Errorf("status = %s after Dead, want dead", row.status)
			}
			if row.deadReason == nil {
				t.Fatal("dead_reason is null after Dead, and the reason was supplied")
			}
			if *row.deadReason != theDeadReason {
				t.Errorf("dead_reason = %s, want the supplied %s", *row.deadReason, theDeadReason)
			}
			assertLeaseCleared(t, row)
		},
	},
}

func TestEachTransitionLeavesTheQueueRowTheContractRequires(t *testing.T) {
	skipIfShort(t)
	for _, outcome := range queueOutcomeCases {
		t.Run(
			outcome.name, func(t *testing.T) {
				pool := freshDatabase(t)
				applySourceMigrations(t, pool)
				source, err := Open(
					t.Context(), pool, harnessConfig(t),
					Options{LeasedBy: "worker", Lease: time.Minute},
				)
				if err != nil {
					t.Fatal(err)
				}
				defer source.Close()
				seed := seedQueueEvent(t, pool, "listener")
				claimed, err := source.Claim(t.Context(), 1)
				if err != nil || len(claimed) != 1 {
					t.Fatalf("claim = %#v, %v", claimed, err)
				}
				before := serverNow(t, pool)
				if err := outcome.apply(t, source, claimed[0]); err != nil {
					t.Fatal(err)
				}
				outcome.assert(t, readQueueRow(t, pool, seed.ID), before)
			},
		)
	}
}
