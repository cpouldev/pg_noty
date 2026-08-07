//go:build integration

package schema

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 20's property and ADR-4's two consequences for the boot: the lock is held
// across the whole run, and the whole run fits in one connection.
//
// Criterion 20 asks for the property rather than the mechanism, so what is asserted here is the only
// thing an operator would ever notice: the next boot completes, on its own, inside a deadline no
// waiting implementation could meet. Why the mechanism works is not this step's subject and is not
// cited here.

const (
	// theBootAfterACrashDeadline bounds the boot that follows a crashed holder. It is deliberately
	// *below* DefaultLockTimeout: any implementation that waited out a bound, or slept before
	// retrying, would take longer than the shortest wait this package can perform, so "eventually"
	// cannot pass for "immediately". The boot it bounds is a re-entrant one over a database already
	// converged, which is a handful of round trips.
	theBootAfterACrashDeadline = 2 * time.Second
	// theOneConnectionDeadline bounds the boot that has exactly one connection to run in. It is
	// generous, because what it catches is not slowness: a boot handing the pool to anything after
	// step 2 waits on an acquire that can never be served, and only a deadline turns that from a
	// hung suite into a failure.
	theOneConnectionDeadline = 30 * time.Second
	// theHeldLockDeadline bounds the wait for the running boot to be seen holding its key.
	theHeldLockDeadline = 15 * time.Second
)

// TestABootAfterACrashedHolderCompletesWithoutWaiting is criterion 20.
func TestABootAfterACrashedHolderCompletesWithoutWaiting(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	cfg := aBootConfiguration(t)
	mustBootstrap(t, pool, cfg)

	// The crashing boot holds the key a later boot needs, on a pinned connection out of a pool of
	// its own, exactly as a boot does -- and then its backend dies.
	crashing, err := OpenPool(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open a pool for the boot that is about to die: %v", err)
	}
	t.Cleanup(crashing.Close)

	holder := acquiredConn(t, crashing)
	key := lockKey(cfg.Database.Schema, cfg.Instance)
	if _, err := acquireLock(t.Context(), holder, key); err != nil {
		t.Fatalf("take the key the crashing boot abandons: %v", err)
	}
	if lockIsFree(t, acquiredConn(t, pool), key) {
		t.Fatal("the key was never held, so the boot below measures no recovery at all")
	}
	terminateBackend(t, pool, backendPIDOf(t, holder))

	bounded, cancel := context.WithTimeout(t.Context(), theBootAfterACrashDeadline)
	defer cancel()

	began := time.Now()
	if err := Bootstrap(bounded, pool, cfg); err != nil {
		t.Fatalf(
			"the boot after a crashed holder answered %v after %s; criterion 20 says it needs "+
				"no operator step and no timeout", err, time.Since(began),
		)
	}
	if elapsed := time.Since(began); elapsed >= theBootAfterACrashDeadline {
		t.Errorf(
			"the boot took %s, which is not inside the %s a non-waiting one has",
			elapsed, theBootAfterACrashDeadline,
		)
	}
}

// TestTheBootHoldsItsLockAcrossTheWholeRunAndGivesItBack is ADR-4's lifetime, asserted as behaviour
// rather than only as the shape heldLock forces. A release issued at the right moment on the wrong
// connection would leave the key held for the life of that pooled connection, and nothing but an
// observation from another session can tell that apart from a correct release.
//
// The run is held open at step 6 by an ACCESS EXCLUSIVE lock on the ledger. That read is the boot's
// one statement outside any lock bound, so the window between step 3 taking the key and step 6
// reading stays open for exactly as long as the fixture keeps it -- which is what makes the
// observation a measurement rather than a race against a boot that is only milliseconds long.
func TestTheBootHoldsItsLockAcrossTheWholeRunAndGivesItBack(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	cfg := aBootConfiguration(t)
	mustBootstrap(t, pool, cfg)

	observer := acquiredConn(t, pool)
	key := lockKey(cfg.Database.Schema, cfg.Instance)
	if !lockIsFree(t, observer, key) {
		t.Fatal("the key is still held after a boot returned, so it was never given back")
	}

	release := holdTheLedgerLocked(t, pool)
	booted := make(chan error, 1)
	go func() { booted <- Bootstrap(t.Context(), pool, cfg) }()

	waitUntilTheKeyIsHeld(t, observer, key)
	release()
	if err := <-booted; err != nil {
		t.Fatalf("the boot answered %v once the ledger was readable again", err)
	}

	if !lockIsFree(t, observer, key) {
		t.Error(
			"the key is still held after the boot returned, so a connection has gone back into " +
				"the pool holding it and every later boot will queue behind an idle session",
		)
	}
}

// holdTheLedgerLocked takes ACCESS EXCLUSIVE on the ledger and answers with its release. It is not
// Step 11's holdTheEventLogLocked, which locks the parent ONLY and therefore blocks no statement a
// boot over a converged schema issues; the subject here is a different relation and a different
// question.
func holdTheLedgerLocked(t *testing.T, pool *pgxpool.Pool) func() {
	t.Helper()

	tx, err := acquiredConn(t, pool).Begin(t.Context())
	if err != nil {
		t.Fatalf("begin the transaction holding the ledger: %v", err)
	}
	mustExecOn(
		t, tx, "LOCK TABLE "+mustQualify(t, harnessSchema, TableSchemaVersion)+
			" IN ACCESS EXCLUSIVE MODE",
	)

	// Registered as well as returned, so a case that never releases early still hands the next one a
	// usable database. t.Context() is cancelled just before cleanups run and a rollback needs one
	// that is not; rolling back twice answers pgx.ErrTxClosed and changes nothing.
	release := func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }
	t.Cleanup(release)
	return release
}

// waitUntilTheKeyIsHeld blocks until a running boot is observably holding one key.
func waitUntilTheKeyIsHeld(t *testing.T, observer *pgxpool.Conn, key int64) {
	t.Helper()

	for deadline := time.Now().Add(theHeldLockDeadline); time.Now().Before(deadline); {
		if !lockIsFree(t, observer, key) {
			return
		}
		time.Sleep(theQueuePollInterval)
	}
	t.Fatalf(
		"no session held key %d within %s while a boot was running, so the lock is not held "+
			"across the run it is meant to guard", key, theHeldLockDeadline,
	)
}

// TestBootstrapCompletesOnAPoolThatCanServeOneConnection is the correctness half of the pinned
// connection, and the reason step 9 is handed that connection rather than the pool.
//
// A pool of one is a configuration a small deployment writes. The boot borrows its single connection
// at step 2 and holds it to step 11, so anything after that step which reached for the pool would
// wait for a connection that cannot be served until the boot it is part of has finished.
func TestBootstrapCompletesOnAPoolThatCanServeOneConnection(t *testing.T) {
	skipIfShort(t)

	freshDatabase(t)
	cfg := aPoolOfOneConfiguration(t, aBootConfiguration(t))

	single, err := OpenPool(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open a pool of one connection: %v", err)
	}
	t.Cleanup(single.Close)

	bounded, cancel := context.WithTimeout(t.Context(), theOneConnectionDeadline)
	defer cancel()

	if err := Bootstrap(bounded, single, cfg); err != nil {
		t.Fatalf(
			"a boot onto a pool of one connection answered %v; everything after step 2 runs on "+
				"the connection step 2 pinned, and a second handle has nothing left to acquire", err,
		)
	}
}

// aPoolOfOneConfiguration is the same configuration with the driver's pool size pinned to one.
func aPoolOfOneConfiguration(t *testing.T, cfg config.Config) config.Config {
	t.Helper()

	separator := "?"
	if strings.Contains(cfg.Database.URL, "?") {
		separator = "&"
	}
	cfg.Database.URL += separator + "pool_max_conns=1"

	conf, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		t.Fatalf("read back the connection string this case wrote: %v", err)
	}
	if conf.MaxConns != 1 {
		t.Fatalf(
			"the driver read a pool of %d connections from %q, so this case would pass on a "+
				"pool that had spare connections all along", conf.MaxConns, cfg.Database.URL,
		)
	}
	return cfg
}
