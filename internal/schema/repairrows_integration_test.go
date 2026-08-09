//go:build integration

package schema

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is which rows a drain moves, which it leaves exactly where they are, and what they
// arrive as.
//
// The two boundary rows are the pair that separates half-open bounds from any other reading, and
// neither implies the other: a row at the
// lower bound belongs to the range and must move, one at the upper bound belongs to the range that
// *starts* there and must not. Both instants come from the alignment arithmetic's own bounds rather
// than being written as literals. The decoy is the other
// half of the same guard: a row in the DEFAULT partition for a *different* blocked range is the
// nearest input the drain must not touch, and without it a drain that emptied the whole DEFAULT
// partition would pass every row above.

// aThreeWayBlockedLog plants the four rows the case below needs and runs the pass that refuses all
// three ranges they block, answering with the ranges and the planted ids.
type threeWayBlockedLog struct {
	pool                          *pgxpool.Pool
	cfg                           config.Config
	drained, adjacent, distant    Range
	atLowerBound, insideTheRange  int64
	atUpperBound, inADistantRange int64
}

func aThreeWayBlockedLog(t *testing.T) threeWayBlockedLog {
	t.Helper()

	pool := migratedSchema(t)
	cfg := harnessConfig(t)
	cfg.Retention = theMaintainedRetention

	before := theClockOf(t, pool)
	wanted := RequiredRanges(before, cfg.Retention)
	planted := threeWayBlockedLog{
		pool: pool, cfg: cfg,
		drained: wanted[0], adjacent: wanted[1], distant: wanted[3],
	}

	if !planted.drained.To.Equal(planted.adjacent.From) {
		t.Fatalf(
			"the first two ranges are %s and %s, which do not meet, so a row written at the "+
				"first one's upper bound is not the second one's lower bound",
			extentOf(planted.drained), extentOf(planted.adjacent),
		)
	}

	interval := cfg.Retention.PartitionInterval
	planted.atLowerBound = plantAndEnqueue(t, pool, planted.drained.From)
	planted.insideTheRange = plantAndEnqueue(t, pool, planted.drained.From.Add(interval/2))
	planted.atUpperBound = plantAndEnqueue(t, pool, planted.drained.To)
	planted.inADistantRange = plantAndEnqueue(t, pool, planted.distant.From.Add(interval/2))

	report := onePass(t, pool, cfg, Options{})
	theHorizonBetween(t, before, theClockOf(t, pool))
	assertRangeBlocked(t, pool, report, planted.drained, "the pass over three blocked ranges")
	return planted
}

// TestADrainMovesItsOwnRangeAndNothingElse is the exclusion in both directions, and then the
// recovery a second time over a non-adjacent range -- so the drain is shown to be range-scoped
// rather than incidentally global.
func TestADrainMovesItsOwnRangeAndNothingElse(t *testing.T) {
	skipIfShort(t)

	planted := aThreeWayBlockedLog(t)
	defer restoreToSnapshot(t)
	was := eventInventory(t, planted.pool)

	done, err := RepairDefaultPartition(t.Context(), planted.pool, planted.cfg, Options{}, planted.drained)
	if err != nil {
		t.Fatalf("drain %s: %v", extentOf(planted.drained), err)
	}
	if done.Moved != 2 {
		t.Errorf(
			"the drain moved %d rows and two of the four planted rows fall inside %s",
			done.Moved, extentOf(planted.drained),
		)
	}

	_, into := identityOf(t, planted.pool, harnessSchema, planted.drained.Name)
	_, stayed := identityOf(t, planted.pool, harnessSchema, PartitionDefault)
	assertLandedIn(
		t, planted.pool, was, map[int64]string{
			planted.atLowerBound:   into,
			planted.insideTheRange: into,
			// A row at the upper bound belongs to the range that starts there, which is still uncovered.
			planted.atUpperBound:    stayed,
			planted.inADistantRange: stayed,
		},
	)
	assertStillUncovered(t, planted)
	assertTheDistantRangeDrainsOnItsOwn(t, planted, stayed)
}

// assertLandedIn is one expectation per planted row rather than a membership test over the set, so
// two rows landing in one partition cannot satisfy the row named for a third. Every row's columns
// are compared too, the ones that never moved included: a drain that rewrote a row it left in
// place is a data-loss defect the routing assertion alone would not see.
func assertLandedIn(t *testing.T, pool *pgxpool.Pool, was map[int64]eventRow, wantIn map[int64]string) {
	t.Helper()

	now := eventInventory(t, pool)
	if len(now) != len(was) {
		t.Fatalf("the log holds %d events and held %d before the drain", len(now), len(was))
	}
	for id, want := range wantIn {
		if now[id].partition != want {
			t.Errorf("event %d is held by %s, want %s", id, now[id].partition, want)
		}
		if now[id].columns != was[id].columns {
			t.Errorf("event %d reads %s and read %s before the drain", id, now[id].columns, was[id].columns)
		}
	}
}

// assertStillUncovered is what the two untouched rows keep blocked. Without it "the row stayed in
// the DEFAULT partition" would be consistent with a drain that created the adjacent range's
// partition too and simply misrouted the row.
func assertStillUncovered(t *testing.T, planted threeWayBlockedLog) {
	t.Helper()

	for _, ranged := range []Range{planted.adjacent, planted.distant} {
		if coversRange(t, planted.pool, ranged) {
			t.Errorf(
				"%s is covered after a drain of %s, and its own blocking row was never moved",
				extentOf(ranged), extentOf(planted.drained),
			)
		}
	}
}

// assertTheDistantRangeDrainsOnItsOwn runs the chain a second time over a range that is not
// adjacent to the first, which is what says the recovery is scoped to the range it was given.
func assertTheDistantRangeDrainsOnItsOwn(t *testing.T, planted threeWayBlockedLog, stayed string) {
	t.Helper()

	done, err := RepairDefaultPartition(t.Context(), planted.pool, planted.cfg, Options{}, planted.distant)
	if err != nil {
		t.Fatalf("drain the second, non-adjacent range %s: %v", extentOf(planted.distant), err)
	}
	if done.Moved != 1 {
		t.Errorf(
			"the second drain moved %d rows and one planted row falls inside %s",
			done.Moved, extentOf(planted.distant),
		)
	}

	_, into := identityOf(t, planted.pool, harnessSchema, planted.distant.Name)
	now := eventInventory(t, planted.pool)
	if now[planted.inADistantRange].partition != into {
		t.Errorf(
			"the distant range's own row is held by %s, want %s",
			now[planted.inADistantRange].partition, into,
		)
	}
	if now[planted.atUpperBound].partition != stayed {
		t.Errorf(
			"the upper-bound row is held by %s after two drains of other ranges, want %s",
			now[planted.atUpperBound].partition, stayed,
		)
	}
}
