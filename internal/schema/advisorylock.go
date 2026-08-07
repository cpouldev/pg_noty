package schema

import (
	"context"
	"hash/fnv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is the package's one lock authority: the key N replicas agree on, and the two
// statements that take and give the lock back. Skill Pattern 1 and ADR-4 are what it implements,
// and both of their corrections are load-bearing rather than preferences.
//
// The scope is the session's, not the transaction's. pg_advisory_xact_lock is the more
// natural-looking choice -- it releases itself, so no unlock can be forgotten -- and it is wrong
// here for a reason specific to this design: each migration runs in its own transaction, so a
// transaction-scoped lock taken inside migration 1 releases at migration 1's COMMIT and leaves
// 2..N running unguarded. Every single-migration test still passes.
// TestASessionScopedAdvisoryLockIsStillHeldAfterACommitOnItsOwnConnection and
// TestATransactionScopedAdvisoryLockIsReleasedByThatSameCommit measure the two scopes separately,
// so choosing the wrong one fails a named row rather than passing quietly.
//
// The connection is one pinned *pgxpool.Conn, not the pool. A session-level lock released from a
// connection other than the one that took it does not fail: the server answers false, warns, and
// leaves the lock held -- and that connection returns to the pool still holding it, invisible until
// connections are reaped (TestUnlockingThroughAFreshPoolCallLeavesTheLockHeld).
//
// Nothing needs to release it on a crash. Backend termination releases a session lock immediately,
// so the next boot's first attempt succeeds with no operator step and no timeout -- criterion 20,
// measured by TestATerminatedHoldersLockIsFreeToTheNextSessionWithoutWaiting.

const (
	// takeTheLock blocks until the key is free, which is what serialises N replicas booting at
	// once. Blocking rather than trying: a replica that gave up would have to decide whether the
	// schema it found was finished, and that is the question the lock exists to avoid asking.
	takeTheLock = "SELECT pg_advisory_lock($1)"
	// giveTheLockBack is only ever issued on the connection that took it, which heldLock is what
	// enforces. The server's answer is not read: it is false exactly when this package has already
	// gone wrong somewhere the return value cannot repair.
	giveTheLockBack = "SELECT pg_advisory_unlock($1)"
	// lockKeySeparator is the byte ADR-3 puts between the two names. Without it ("not", "yorders")
	// and ("noty", "orders") hash alike, and two unrelated deployments would block one another with
	// nothing in either log to say why. NUL cannot occur inside either name, because identifier.go
	// refuses a name holding one before it can reach any statement.
	lockKeySeparator = "\x00"
)

type lockDomain string

const (
	bootstrapDomain lockDomain = ""
	reconcileDomain lockDomain = "reconcile\x00"
)

// lockDomains is closed: identifier.go refuses names holding a NUL before they reach a statement,
// so no (schema, instance) can forge "reconcile\x00"+schema+lockKeySeparator+instance under the
// bootstrap domain. An equal key would therefore be an FNV collision, not a design defect.
var lockDomains = []lockDomain{bootstrapDomain, reconcileDomain}

// lockKey is the advisory-lock key for one instance's custody of one schema: fnv1a64 over the
// schema, a NUL and the instance (ADR-3).
//
// Both names are in it, and each for its own reason. The instance is what criterion 21 rests on --
// two instances sharing a database must not serialise against each other, and a key ignoring it
// makes them. The schema is what keeps two instances that *do* share a schema apart from two that
// merely share a server; the residual case, one schema claimed by two instances, is closed by the
// ownership marker rather than by this key, because a key cannot refuse anything.
//
// The result is signed, and half the key space is negative. That is not a defect to correct: the
// server's argument is a bigint, so a negative key is as good as a positive one
// (TestANegativeLockKeyIsOneTheServerLocksOn), and folding the range would map two digests onto one
// key for no gain.
func lockKeyIn(domain lockDomain, schema, instance string) int64 {
	digest := fnv.New64a()
	// hash.Hash's Write never returns an error, which its own documentation states; the value is
	// discarded here rather than checked so that the one line reads as the hash it is.
	_, _ = digest.Write([]byte(string(domain) + schema + lockKeySeparator + instance))

	return int64(digest.Sum64())
}

func lockKey(schema, instance string) int64 { return lockKeyIn(bootstrapDomain, schema, instance) }

// ReconcileLockKey derives the run-lock key under Correction 3 and criterion 42's separate domain.
func ReconcileLockKey(schema, instance string) int64 {
	return lockKeyIn(reconcileDomain, schema, instance)
}

// heldLock is a session-scoped advisory lock together with the one connection that holds it.
//
// The connection is a field rather than a parameter of release, and that is the whole design: a
// release cannot be issued on the wrong handle because it is never given one. The zero value holds
// no connection and releasing it panics -- deliberately, in the same way RequiredRanges panics on a
// non-positive interval. acquireLock answers with a usable value only alongside a nil error, and a
// caller that ignored the error has a defect that a silent no-op would hide until every future boot
// blocked.
type heldLock struct {
	on  *pgxpool.Conn
	key int64
}

// acquireLock takes the lock for one key on one pinned connection, blocking until it is free.
func acquireLock(ctx context.Context, on *pgxpool.Conn, key int64) (heldLock, error) {
	if _, err := on.Exec(ctx, takeTheLock, key); err != nil {
		return heldLock{}, finished(err)
	}
	return heldLock{on: on, key: key}, nil
}

// release gives the lock back on the connection that took it.
//
// A caller defers this and is right to: the connection is going back to the pool either way, and a
// lock left on it outlives every process that could have noticed.
func (held heldLock) release(ctx context.Context) error {
	if _, err := held.on.Exec(ctx, giveTheLockBack, held.key); err != nil {
		return finished(err)
	}
	return nil
}
