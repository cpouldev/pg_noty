//go:build integration

package schema

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 40's mechanism and the fixtures the rest of this step's DDL cases are
// written on.
//
// The fixture is hand-built rather than this package's own migration corpus, and deliberately: a
// classification tested through the corpus is a test of Step 6's DDL as much as of the classifier,
// and when it fails neither is ruled out.

const (
	// probeParent is a partitioned table shaped like noty.events -- a range-partitioned parent with
	// a permanent DEFAULT partition -- and nothing else about it is this package's.
	probeParent    = "probe_events"
	probeDefault   = "probe_events_default"
	probePartition = "probe_events_march"
)

// conflictingBound is the timeout the conflicting-lock rows configure. Short, because the row is
// about the abort rather than about the wait, and every second here is a second of suite.
const conflictingBound = 300 * time.Millisecond

// corroborationMargin is the slack the elapsed-time check allows. It is generous on purpose: the
// error classification is the evidence and the clock is corroboration, so this bound exists to
// catch a statement that was never bounded at all, not to measure scheduling
// (the step's own Risks entry).
const corroborationMargin = 5 * time.Second

// theInventoryQuery is every relation the public schema holds, with its kind and its partition
// bound, folded into one ordered value so two readings compare directly. A whole inventory rather
// than the one object the statement targeted, because "the schema is unchanged" is what makes a
// retry safe and a single-object check would miss anything else the attempt left behind.
const theInventoryQuery = `SELECT coalesce(string_agg(entry, E'\n' ORDER BY entry), '') FROM (
	SELECT c.relname || ' ' || c.relkind::text || ' ' || coalesce(pg_get_expr(c.relpartbound, c.oid), '-') AS entry
	FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public') inventory`

// aPartitionedTable builds the parent and its permanent DEFAULT partition.
func aPartitionedTable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	mustExecOn(t, pool, "CREATE TABLE "+probeParent+
		" (id bigint, occurred_at timestamptz NOT NULL) PARTITION BY RANGE (occurred_at)")
	mustExecOn(t, pool, "CREATE TABLE "+probeDefault+" PARTITION OF "+probeParent+" DEFAULT")
}

// createProbePartition is the statement under test: a create, which M5 measured taking ACCESS
// EXCLUSIVE on the parent and on the DEFAULT partition both.
func createProbePartition() string {
	return "CREATE TABLE " + probePartition + " PARTITION OF " + probeParent +
		" FOR VALUES FROM ('2026-03-01') TO ('2026-04-01')"
}

// creating is what a caller in Step 11 will write: the attempt an operator reads in a timeout, and
// the partition an operator drains after a DEFAULT refusal.
func creating(partition string) ddlSubject {
	return ddlSubject{operation: "create partition " + partition, partition: partition}
}

// holdAConflictingLock takes ACCESS EXCLUSIVE on the parent and answers with the release, which a
// caller that goes on to read a partition bound back has to run first.
//
// Measured on 17.10 and worth knowing two steps from here: pg_get_expr(relpartbound, oid) blocks
// behind ACCESS EXCLUSIVE on the relation whose bound it deparses, while a plain pg_class read of
// the same rows does not. So an inventory that reads bounds cannot be taken while this lock is
// held, and one taken anyway hangs rather than failing.
func holdAConflictingLock(t *testing.T, pool *pgxpool.Pool) func() {
	t.Helper()

	tx, err := acquiredConn(t, pool).Begin(t.Context())
	if err != nil {
		t.Fatalf("begin the transaction holding the conflicting lock: %v", err)
	}
	mustExecOn(t, tx, "LOCK TABLE "+probeParent+" IN ACCESS EXCLUSIVE MODE")

	// Registered as well as returned, so a case that never releases early still hands the next one
	// a usable database. t.Context() is cancelled just before cleanups run and a rollback needs one
	// that is not; rolling back twice answers pgx.ErrTxClosed and changes nothing.
	release := func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }
	t.Cleanup(release)
	return release
}

// inventoryOf reads the whole public schema as one comparable value.
func inventoryOf(t *testing.T, on runner) string {
	t.Helper()

	var inventory string
	if err := on.QueryRow(t.Context(), theInventoryQuery).Scan(&inventory); err != nil {
		t.Fatalf("read the schema inventory: %v", err)
	}
	return inventory
}

// TestAStatementMeetingAConflictingLockGivesUpAndLeavesTheSchemaUnchanged is criterion 40's
// mechanism. The classification is the evidence; the clock only corroborates it, because a
// wall-clock assertion read as primary evidence is flaky on a loaded machine and a flake here reads
// as an infrastructure problem rather than as the wrong assertion it is.
func TestAStatementMeetingAConflictingLockGivesUpAndLeavesTheSchemaUnchanged(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	aPartitionedTable(t, pool)

	// The inventory is read on either side of the whole episode rather than inside it, because the
	// lock the episode is about is one the bound-reading half of the inventory would itself wait on.
	before := inventoryOf(t, pool)
	release := holdAConflictingLock(t, pool)

	began := time.Now()
	refused := boundedTx(t.Context(), pool, Options{LockTimeout: conflictingBound},
		creating(probePartition), oneStatement(createProbePartition()))
	elapsed := time.Since(began)
	release()

	if !errors.Is(refused, ErrLockTimeout) {
		t.Fatalf("the create answered %v, want %v; a conflicting ACCESS EXCLUSIVE lock was held "+
			"for the whole attempt", refused, ErrLockTimeout)
	}
	if errors.Is(refused, ErrDefaultBlocked) {
		t.Errorf("the timeout also matches %v, so the two conditions are one error with two "+
			"messages and Steps 11, 14 and 15 cannot assert against either", ErrDefaultBlocked)
	}
	if elapsed > conflictingBound+corroborationMargin {
		t.Errorf("the statement gave up after %s, which is more than its %s bound plus %s of "+
			"slack; the bound may not have reached the server at all",
			elapsed, conflictingBound, corroborationMargin)
	}
	if after := inventoryOf(t, pool); after != before {
		t.Errorf("the schema changed across a statement that timed out, so a later pass cannot "+
			"simply retry.\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestTheBoundIsLocalToItsTransactionAndDoesNotOutliveIt is the SET LOCAL half.
//
// A pooled connection outlives every statement that ran on it, so a session-level SET would bound
// the next caller's unrelated work at whatever this pass configured and keep doing so until that
// connection was reaped. The reading afterwards is compared against the one taken before rather than
// against a written-down default, so the row holds whatever the server's own default is.
func TestTheBoundIsLocalToItsTransactionAndDoesNotOutliveIt(t *testing.T) {
	skipIfShort(t)

	pinned := acquiredConn(t, freshDatabase(t))
	const configured = 1234 * time.Millisecond
	settingBefore := lockTimeoutMillisecondsOn(t, pinned)

	var inside int64
	err := boundedTx(t.Context(), pinned, Options{LockTimeout: configured}, creating(probePartition),
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, theLockTimeoutQuery).Scan(&inside)
		})
	if err != nil {
		t.Fatalf("read the bound from inside the transaction it bounds: %v", err)
	}

	if want := configured.Milliseconds(); inside != want {
		t.Errorf("the transaction ran under a lock_timeout of %d ms, want the %d ms configured",
			inside, want)
	}
	if after := lockTimeoutMillisecondsOn(t, pinned); after != settingBefore {
		t.Errorf("lock_timeout is %d ms on the connection after the transaction and was %d ms "+
			"before it, so the bound was set for the session rather than for the statement",
			after, settingBefore)
	}
}

// theLockTimeoutQuery reads the bound in the unit pg_settings records it in, which is stable across
// renderings: SHOW writes 3000 milliseconds as `3s` and 60000 as `1min`, and a test comparing those
// spellings would be measuring the renderer.
const theLockTimeoutQuery = `SELECT setting::bigint FROM pg_settings WHERE name = 'lock_timeout'`

// lockTimeoutMillisecondsOn is the bound one handle currently runs under.
func lockTimeoutMillisecondsOn(t *testing.T, on runner) int64 {
	t.Helper()

	var milliseconds int64
	if err := on.QueryRow(t.Context(), theLockTimeoutQuery).Scan(&milliseconds); err != nil {
		t.Fatalf("read lock_timeout: %v", err)
	}
	return milliseconds
}
