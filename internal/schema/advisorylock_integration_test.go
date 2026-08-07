//go:build integration

package schema

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is M7, measured in both directions, plus the handles the rest of this step's
// container-backed cases are written on.
//
// Both directions are here because one alone cannot tell the two mechanisms apart, and choosing the
// wrong one is the defect this step exists to make impossible: a transaction-scoped lock releases at
// migration 1's COMMIT and leaves migrations 2..N unguarded, while every single-migration test still
// passes.

// acquiredConn pins one connection out of the pool for the whole test, which is the shape ADR-4
// requires of everything that touches the lock.
func acquiredConn(t *testing.T, pool *pgxpool.Pool) *pgxpool.Conn {
	t.Helper()

	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("pin a connection out of the pool: %v", err)
	}
	t.Cleanup(conn.Release)
	return conn
}

// backendPIDOf is the server-side process one connection is served by, which is what makes "a
// different session" a measurement rather than an assumption.
func backendPIDOf(t *testing.T, conn *pgxpool.Conn) int32 {
	t.Helper()

	var pid int32
	if err := conn.QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("read the backend pid: %v", err)
	}
	return pid
}

// anObserverOf is a connection that is certainly not the one holding the lock.
//
// An advisory lock is re-entrant within a session, so an observer that happened to be the holder
// would report every held lock as free -- and every row in this step would pass while measuring
// nothing.
func anObserverOf(t *testing.T, pool *pgxpool.Pool, holder *pgxpool.Conn) *pgxpool.Conn {
	t.Helper()

	observer := acquiredConn(t, pool)
	if observing, holding := backendPIDOf(t, observer), backendPIDOf(t, holder); observing == holding {
		t.Fatalf("the observer is the holder's own backend (pid %d), and an advisory lock is "+
			"re-entrant within one session", holding)
	}
	return observer
}

// lockIsFree reports whether one advisory key can be taken from a session that is not the holder.
//
// pg_try_advisory_lock rather than a pg_locks query, because it is what the next boot would do and
// because it answers without waiting -- a blocking probe would turn a held lock into a hung test.
// Anything it took it gives back, so one observer can be asked repeatedly.
func lockIsFree(t *testing.T, observer *pgxpool.Conn, key int64) bool {
	t.Helper()

	var took bool
	if err := observer.QueryRow(t.Context(), "SELECT pg_try_advisory_lock($1)", key).Scan(&took); err != nil {
		t.Fatalf("try the advisory lock: %v", err)
	}
	if took {
		mustExecOn(t, observer, "SELECT pg_advisory_unlock($1)", key)
	}
	return took
}

// commitAnEmptyTransaction runs the smallest transaction there is on one connection, so that a
// COMMIT happens and nothing else does.
func commitAnEmptyTransaction(t *testing.T, on *pgxpool.Conn) {
	t.Helper()

	tx, err := on.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin a transaction on the pinned connection: %v", err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit it: %v", err)
	}
}

// TestASessionScopedAdvisoryLockIsStillHeldAfterACommitOnItsOwnConnection is M7's first direction,
// in the shape bootstrap uses it: the lock is taken through this package's own acquireLock, outside
// any transaction, and the migrations then run one transaction each on that same connection.
func TestASessionScopedAdvisoryLockIsStillHeldAfterACommitOnItsOwnConnection(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	pinned := acquiredConn(t, pool)
	observer := anObserverOf(t, pool, pinned)
	key := lockKey(harnessSchema, "session-scope")

	if _, err := acquireLock(t.Context(), pinned, key); err != nil {
		t.Fatalf("take the session lock: %v", err)
	}
	if lockIsFree(t, observer, key) {
		t.Fatal("the lock was never held, so nothing below measures whether a COMMIT releases it")
	}

	// Two commits rather than one, so surviving a COMMIT is established as a property rather than
	// as a single observation: bootstrap runs one transaction per migration on this connection, and
	// a lock released by the second COMMIT would leave migrations 3..N unguarded exactly as a
	// transaction-scoped one leaves 2..N.
	for round := 1; round <= 2; round++ {
		commitAnEmptyTransaction(t, pinned)

		if lockIsFree(t, observer, key) {
			t.Fatalf("the session lock was gone after commit %d; a session-scoped lock survives "+
				"COMMIT, and one that does not guards only the migration it was taken inside", round)
		}
	}
}

// TestATransactionScopedAdvisoryLockIsReleasedByThatSameCommit is M7's opposite direction, and it is
// what makes the row above mean something: without it, an implementation that took the transaction
// scope would be indistinguishable from one that took the session scope in every test that applies
// a single migration.
func TestATransactionScopedAdvisoryLockIsReleasedByThatSameCommit(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	pinned := acquiredConn(t, pool)
	observer := anObserverOf(t, pool, pinned)
	key := lockKey(harnessSchema, "transaction-scope")

	tx, err := pinned.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin the transaction the lock is scoped to: %v", err)
	}
	mustExecOn(t, tx, "SELECT pg_advisory_xact_lock($1)", key)

	if lockIsFree(t, observer, key) {
		t.Fatal("the transaction-scoped lock was not held inside its own transaction, so the row " +
			"below cannot tell a release from a failure to take it")
	}

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit the transaction the lock is scoped to: %v", err)
	}

	if !lockIsFree(t, observer, key) {
		t.Error("the transaction-scoped lock outlived its COMMIT, so the two scopes are no longer " +
			"distinguishable here and taking the wrong one in bootstrap would pass quietly")
	}
}
