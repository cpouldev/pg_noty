//go:build integration

package schema

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 20's mechanism: a bootstrap that dies holding the lock leaves nothing
// behind for the next one to wait on. The criterion asks for the property rather than the mechanism,
// and the property is asserted here as the only thing an operator would ever notice -- the next boot
// takes the lock, on its first attempt, without waiting.
//
// The negative-key row lives here too, because it is the other half of "the key this package
// computes is one the server can lock on": half the bigint range is negative and nothing in
// advisorylock.go may treat that as an error.

// reacquisitionDeadline bounds the one attempt the next boot makes.
//
// It is tight on purpose. Criterion 20's whole claim is that no operator step and no timeout is
// needed, and "eventually" passes for "immediately" against any deadline generous enough to hide a
// retry: a backoff loop's first sleep is measured in seconds. A single round trip against a
// container on the same machine costs roughly a millisecond, so this leaves two orders of magnitude
// of headroom for a loaded machine and none at all for a wait.
const reacquisitionDeadline = 250 * time.Millisecond

// terminationWait is how long the terminating session waits for the holder to actually be gone.
//
// pg_terminate_backend(pid) signals and returns; the two-argument form waits for the backend to
// finish dying. Waiting is what keeps the measurement below about the *lock* rather than about a
// race between a signal and a probe: the crash is this test's setup, and the property is what the
// next boot meets afterwards.
const terminationWait = 5 * time.Second

// TestATerminatedHoldersLockIsFreeToTheNextSessionWithoutWaiting is criterion 20.
func TestATerminatedHoldersLockIsFreeToTheNextSessionWithoutWaiting(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	key := lockKey(harnessSchema, "crashed-holder")

	// The holder gets its own pool, so terminating its backend cannot leave the pool the next boot
	// uses holding a connection to a process that no longer exists.
	crashing, err := OpenPool(t.Context(), harnessConfig(t))
	if err != nil {
		t.Fatalf("open a pool for the bootstrap that is about to die: %v", err)
	}
	// Registered as a cleanup rather than deferred, and the difference is a ten-minute hang: Close
	// waits for every connection to be handed back, acquiredConn hands its own back through a
	// cleanup, and a defer runs before every cleanup rather than after them.
	t.Cleanup(crashing.Close)

	holder := acquiredConn(t, crashing)
	if _, err := acquireLock(t.Context(), holder, key); err != nil {
		t.Fatalf("take the lock that is about to be abandoned: %v", err)
	}

	next := acquiredConn(t, pool)
	if lockIsFree(t, next, key) {
		t.Fatal("the lock was never held, so nothing below measures a release")
	}

	terminateBackend(t, pool, backendPIDOf(t, holder))
	assertTakenOnTheFirstAttempt(t, next, key)
}

// terminateBackend kills one backend and waits for it to be gone, standing in for a bootstrap
// process dying mid-run.
func terminateBackend(t *testing.T, pool *pgxpool.Pool, pid int32) {
	t.Helper()

	var terminated bool
	err := pool.QueryRow(t.Context(), "SELECT pg_terminate_backend($1, $2)",
		pid, terminationWait.Milliseconds()).Scan(&terminated)
	if err != nil {
		t.Fatalf("terminate backend %d: %v", pid, err)
	}
	if !terminated {
		t.Fatalf("backend %d was still alive after %s, so the row below would measure a lock that "+
			"was never abandoned", pid, terminationWait)
	}
}

// assertTakenOnTheFirstAttempt makes exactly one non-blocking attempt at the lock, under a deadline
// no waiting implementation could meet.
//
// One attempt rather than a loop, because pg_try_advisory_lock is non-blocking by definition: a
// single call answering true *is* the proof that no wait was needed, and a loop would turn
// "immediately" back into "eventually" however the elapsed time were asserted afterwards.
func assertTakenOnTheFirstAttempt(t *testing.T, on *pgxpool.Conn, key int64) {
	t.Helper()

	bounded, cancel := context.WithTimeout(t.Context(), reacquisitionDeadline)
	defer cancel()

	began := time.Now()
	var took bool
	err := on.QueryRow(bounded, "SELECT pg_try_advisory_lock($1)", key).Scan(&took)
	elapsed := time.Since(began)

	if err != nil {
		t.Fatalf("the next boot's single attempt at the lock failed after %s: %v; criterion 20 "+
			"says it needs no operator step and no timeout", elapsed, err)
	}
	if !took {
		t.Fatalf("the next boot could not take the lock %s after its holder was terminated, so a "+
			"crashed bootstrap does leave a lock behind", elapsed)
	}
	if elapsed >= reacquisitionDeadline {
		t.Errorf("the attempt took %s, which is not inside the %s a non-waiting one has",
			elapsed, reacquisitionDeadline)
	}
}

// TestANegativeLockKeyIsOneTheServerLocksOn closes the half of the key space a positive-only test
// leaves untouched. fnv1a64 puts half its digests above the signed range, and every one of those
// configurations would fail at the first boot if the conversion or the server disagreed.
func TestANegativeLockKeyIsOneTheServerLocksOn(t *testing.T) {
	skipIfShort(t)

	schema, instance := aNegativelyKeyedInstance(t)
	key := lockKey(schema, instance)
	if key >= 0 {
		t.Fatalf("lockKey(%q, %q) = %d, which is not negative; this row would repeat the positive "+
			"case", schema, instance, key)
	}

	pool := freshDatabase(t)
	pinned := acquiredConn(t, pool)
	observer := anObserverOf(t, pool, pinned)

	held, err := acquireLock(t.Context(), pinned, key)
	if err != nil {
		t.Fatalf("take the lock on negative key %d: %v", key, err)
	}
	if lockIsFree(t, observer, key) {
		t.Errorf("the lock on negative key %d reads as free to another session", key)
	}

	if err := held.release(t.Context()); err != nil {
		t.Fatalf("release the lock on negative key %d: %v", key, err)
	}
	if !lockIsFree(t, observer, key) {
		t.Errorf("the lock on negative key %d survived its release", key)
	}
}
