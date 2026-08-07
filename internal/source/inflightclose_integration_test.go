//go:build integration

package source

import (
	"context"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The in-flight half of the lifecycle boundary clause. Close blocks until the listening
// goroutine has returned and explicitly does *not* wait for an in-flight Claim/Ack/Nack/Dead, and
// eventsource.go's doc comment says so -- but no case had ever had a call in flight when Close ran,
// so both halves of that sentence were prose. Here an Ack is parked inside the database while Close
// runs, and the two facts are separated: Close returns while the Ack is still there, and the Ack
// then commits with its documented effect rather than being torn down.
//
// The Ack is held by a row lock rather than by a sleep, so "in flight" is the server's own answer --
// pg_blocking_pids -- rather than a guess about scheduling.
const (
	// theBlockedCallDeadline is how long the server is watched for the Ack to reach the row lock, and
	// how long the Ack is then given to finish once the lock is released.
	theBlockedCallDeadline = 10 * time.Second
	// theInFlightListener names the seeded event this case moves through Ack.
	theInFlightListener = "inflight"
)

// lockTheQueueRow holds a row lock on one queue row from a connection of its own -- the pool's
// connections are what the Ack under test needs -- and answers with the release.
func lockTheQueueRow(t *testing.T, cfg config.Config, id int64) func() {
	t.Helper()
	connection, err := pgx.Connect(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open the lock holder's connection: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close(context.Background()) })
	tx, err := connection.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin the lock holder's transaction: %v", err)
	}
	queue := mustQualifyServiceTable(t, harnessSchema, schema.TableEventQueue)
	mustExecOn(t, tx, "SELECT event_id FROM "+queue+" WHERE event_id=$1 FOR UPDATE", id)
	return func() {
		if err := tx.Rollback(context.Background()); err != nil {
			t.Errorf("release the row lock: %v", err)
		}
	}
}

func blockedBackendCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	return countOn(
		t, pool, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() "+
			"AND cardinality(pg_blocking_pids(pid)) > 0",
	)
}

// waitUntilACallIsBlockedOnTheRowLock is what makes the case about a call in flight. Without it
// Close could be called before the Ack had reached the database at all, and the assertions below
// would be about a call that had already finished or not yet started.
func waitUntilACallIsBlockedOnTheRowLock(t *testing.T, pool *pgxpool.Pool, inFlight <-chan error) {
	t.Helper()
	for deadline := time.Now().Add(theBlockedCallDeadline); time.Now().Before(deadline); {
		select {
		case err := <-inFlight:
			t.Fatalf(
				"the Ack returned %v before it was ever blocked, so nothing is in flight and "+
					"Close below is called against an idle source", err,
			)
		default:
		}
		if blockedBackendCount(t, pool) > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf(
		"no backend was blocked within %s, so the Ack never reached the row lock",
		theBlockedCallDeadline,
	)
}

func TestACallInFlightWhenCloseIsCalledStillCompletes(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	source := openListeningSource(t, pool, harnessConfig(t), newListenLog())
	seeded := seedQueueEvent(t, pool, theInFlightListener)
	claimed, err := source.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v; want the seeded event claimed before the boundary", claimed, err)
	}
	// The other side of the blocked-backend counter: nothing is blocked until the Ack meets the lock,
	// so a counter that answered "blocked" whatever the server was doing fails here.
	if got := blockedBackendCount(t, pool); got != 0 {
		t.Fatalf(
			"%d backends were already blocked before the row was locked, so the wait below says "+
				"nothing about the Ack", got,
		)
	}

	release := lockTheQueueRow(t, harnessConfig(t), seeded.ID)
	acked := make(chan error, 1)
	go func() { acked <- source.Ack(t.Context(), claimed[0], theBoundaryDelivery) }()
	waitUntilACallIsBlockedOnTheRowLock(t, pool, acked)

	assertCloseDoesNotWaitForTheCall(t, source)
	release()
	assertTheInFlightCallCompleted(t, pool, acked, seeded.ID)
}

// assertCloseDoesNotWaitForTheCall is the contract's "does not wait for in-flight
// Claim/Ack/Nack/Dead", asserted rather than quoted. Close is driven off the test goroutine so a Close that did wait is a
// named deadline rather than a hang that would take the rest of the package's cases down with it.
func assertCloseDoesNotWaitForTheCall(t *testing.T, source *TriggerSource) {
	t.Helper()
	returned := make(chan error, 1)
	go func() { returned <- source.Close() }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("Close returned %v while a transition was in flight, want nil", err)
		}
	case <-time.After(theCloseDeadline):
		t.Fatalf(
			"Close did not return within %s while an Ack was parked inside the database: it "+
				"waits for the listening goroutine, and for in-flight transitions it must not",
			theCloseDeadline,
		)
	}
}

// assertTheInFlightCallCompleted reads the Ack's own outcome off the queue and the delivery log, so
// a call that returned nil after Close without doing its work fails here.
func assertTheInFlightCallCompleted(t *testing.T, pool *pgxpool.Pool, acked <-chan error, id int64) {
	t.Helper()
	select {
	case err := <-acked:
		if err != nil {
			t.Fatalf(
				"the Ack that was in flight when Close ran returned %v; a call already running "+
					"completes rather than being torn down by the lifecycle ending under it", err,
			)
		}
	case <-time.After(theBlockedCallDeadline):
		t.Fatalf(
			"the in-flight Ack never returned within %s of its lock being released",
			theBlockedCallDeadline,
		)
	}
	if row := readQueueRow(t, pool, id); row.present {
		t.Errorf(
			"the in-flight Ack left the queue row behind with status %s, so it returned nil "+
				"without the deletion Ack owes", row.status,
		)
	}
	if got := deliveryCount(t, pool, id); got != 1 {
		t.Errorf("the in-flight Ack recorded %d delivery attempts, want the 1 it was handed", got)
	}
}
