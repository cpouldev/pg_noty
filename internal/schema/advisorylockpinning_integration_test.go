//go:build integration

package schema

import (
	"testing"
)

// This file is ADR-4's pinned-connection discipline, and the negative case is the evidence rather
// than the positive one.
//
// A session lock unlocked from a connection other than the one that took it does not fail loudly:
// the server answers false, warns, and leaves the lock exactly where it was. The connection then
// goes back into the pool still holding it, and nothing notices until connections are reaped -- at
// which point every future boot blocks on a lock whose holder is an idle pooled connection.
// heldLock exists to make that unrepresentable: release takes no connection argument, because the
// only connection it could correctly use is the one acquireLock already recorded.

// TestUnlockingThroughAFreshPoolCallLeavesTheLockHeld is the failure mode ADR-4 exists to prevent.
func TestUnlockingThroughAFreshPoolCallLeavesTheLockHeld(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	pinned := acquiredConn(t, pool)
	observer := anObserverOf(t, pool, pinned)
	key := lockKey(harnessSchema, "pinned-connection")

	if _, err := acquireLock(t.Context(), pinned, key); err != nil {
		t.Fatalf("take the session lock on the pinned connection: %v", err)
	}

	// The pid and the unlock are read in one round trip, so the two cannot come from different
	// connections: a pool hands out whichever is free, and this row means nothing unless the one it
	// handed out is provably not the holder.
	var servedBy int32
	var unlocked bool
	err := pool.QueryRow(t.Context(), "SELECT pg_backend_pid(), pg_advisory_unlock($1)", key).
		Scan(&servedBy, &unlocked)
	if err != nil {
		t.Fatalf("unlock through a fresh pool call: %v", err)
	}

	if holding := backendPIDOf(t, pinned); servedBy == holding {
		t.Fatalf("the pool call was served by the holder's own backend (pid %d), so this row "+
			"measured a correct unlock rather than a misdirected one", holding)
	}
	if unlocked {
		t.Errorf("the server reported the pool's unlock as successful; measured on 17.10 it answers " +
			"false and warns, because the lock belongs to the session that took it")
	}
	if lockIsFree(t, observer, key) {
		t.Error("an unlock issued through the pool released the lock, so nothing in this package " +
			"would notice a bootstrap that unlocked on the wrong connection")
	}
}

// TestUnlockingOnThePinnedConnectionReleasesIt is the other side of the same guard: without it a
// release that never releases anything would satisfy the row above.
func TestUnlockingOnThePinnedConnectionReleasesIt(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	pinned := acquiredConn(t, pool)
	observer := anObserverOf(t, pool, pinned)
	key := lockKey(harnessSchema, "pinned-release")

	held, err := acquireLock(t.Context(), pinned, key)
	if err != nil {
		t.Fatalf("take the session lock on the pinned connection: %v", err)
	}
	if lockIsFree(t, observer, key) {
		t.Fatal("the lock was never held, so the release below would read as successful whatever " +
			"it did")
	}

	if err := held.release(t.Context()); err != nil {
		t.Fatalf("release on the connection that took it: %v", err)
	}

	if !lockIsFree(t, observer, key) {
		t.Error("the lock survived a release issued on its own connection, so a bootstrap would " +
			"leave it held for the life of that connection")
	}
}
