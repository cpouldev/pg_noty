//go:build integration

package schema

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// This file is D3's behavioural half of DefaultLockTimeout, and only that half.
//
// The constant is pinned twice from two sides and neither side may cite the other's row: Step 4
// asserts the literal the Contracts block names, and this file asserts what a caller who configured
// nothing actually runs under -- the value the server was told, and the wait a real conflicting lock
// produces. A literal that drifted without its behaviour, or a normalisation that quietly stopped
// applying it, fails on this side without Step 4's row being touched.
//
// DefaultLockTimeout is consumed here and nowhere redeclared; options.go is its one declaration.

// boundUndershootTolerance is how far below the configured bound an abort may land and still be
// that bound. The server waits the whole of lock_timeout before it aborts, so the only slack a
// correct run needs is the round trip -- which is why this is small where corroborationMargin, whose
// job is to catch a statement that was never bounded at all, is generous.
const boundUndershootTolerance = 100 * time.Millisecond

// theShownAndCountedBound reads the bound as an operator would see it and as the server counts it.
// Both, because the criterion names SHOW and SHOW is a renderer -- 3000 milliseconds render as `3s`
// and 60000 as `1min` -- so a comparison of spellings would be measuring the rendering rather than
// the bound.
const theShownAndCountedBound = `SELECT current_setting('lock_timeout'),
	(SELECT setting::bigint FROM pg_settings WHERE name = 'lock_timeout')`

// TestAZeroValuedOptionsRunsTheTransactionUnderTheDefaultLockTimeout is the configured half.
func TestAZeroValuedOptionsRunsTheTransactionUnderTheDefaultLockTimeout(t *testing.T) {
	skipIfShort(t)

	var shown string
	var counted int64
	err := boundedTx(t.Context(), freshDatabase(t), Options{}, creating(probePartition),
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, theShownAndCountedBound).Scan(&shown, &counted)
		})
	if err != nil {
		t.Fatalf("read the bound from inside a transaction opened with a zero-valued Options: %v", err)
	}

	if want := DefaultLockTimeout.Milliseconds(); counted != want {
		t.Errorf("a caller who configured nothing ran under a lock_timeout of %d ms, shown as %q, "+
			"want the %d ms DefaultLockTimeout names; a zero Options must reach the server as the "+
			"default rather than as no bound at all", counted, shown, want)
	}
}

// TestAZeroValuedOptionsGivesUpAgainstARealConflictingLockAtTheDefaultBound is the behavioural half.
//
// It is not the row above restated. That row proves the server was *told* the default; this one
// proves the default is what the statement then *waits*, which is what criterion 40 is about and
// what a configured-but-ignored setting would fail.
func TestAZeroValuedOptionsGivesUpAgainstARealConflictingLockAtTheDefaultBound(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	aPartitionedTable(t, pool)
	holdAConflictingLock(t, pool)

	began := time.Now()
	refused := boundedTx(t.Context(), pool, Options{}, creating(probePartition),
		oneStatement(createProbePartition()))
	elapsed := time.Since(began)

	if !errors.Is(refused, ErrLockTimeout) {
		t.Fatalf("the create answered %v, want %v", refused, ErrLockTimeout)
	}

	// The lower bound is what separates the default from any shorter value a normalisation might
	// have substituted; the upper bound is what separates it from no bound at all. Neither is the
	// primary evidence -- the classification above is -- but together they are the only way to see
	// *which* bound was applied.
	if elapsed < DefaultLockTimeout-boundUndershootTolerance {
		t.Errorf("the create gave up after %s, short of the %s a zero-valued Options configures by "+
			"more than the %s a round trip can account for; a shorter bound was substituted",
			elapsed, DefaultLockTimeout, boundUndershootTolerance)
	}
	if elapsed > DefaultLockTimeout+corroborationMargin {
		t.Errorf("the create gave up after %s, which is more than the %s a zero-valued Options "+
			"configures plus %s of slack", elapsed, DefaultLockTimeout, corroborationMargin)
	}
}
