//go:build integration

package schema

import (
	"slices"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criteria 34 and 35 realised on disk. Every expected drop set here is derived from the
// alignment arithmetic and the configuration, with the derivation written beside it, and none is
// read back from what a run happened to drop: a recorded drop set ratifies whatever was dropped --
// including a partition that should have survived -- and then defends that against every later fix,
// which on this step means defending a data-loss defect.

// theAgeSpread is how many whole intervals back the AC 34 fixture reaches: twice the retention
// window, so the set runs from a partition holding now to one far older than any cutoff, and the
// boundary sits among neighbours rather than alone.
const theAgeSpread = 12

// stillOnTheSameGridLine guards a case that read the clock, planted against it and then let a pass
// read the clock again. A pass that crossed a boundary decided against a different k than the
// expectation was derived from, and then the expectation rather than the pass is what was wrong.
func stillOnTheSameGridLine(t *testing.T, before, after time.Time, interval time.Duration) {
	t.Helper()

	if gridIndex(before, interval) != gridIndex(after, interval) {
		t.Fatalf(
			"the pass ran across a %s grid boundary, between %s and %s; every expectation here "+
				"is derived from the instant before it, so this run proves nothing -- re-run it",
			interval, before, after,
		)
	}
}

// TestExactlyTheWhollyExpiredPartitionsAreDroppedAcrossAWideAgeSpread is criterion 34.
//
// The derivation, written out so a reader can check it without running anything. Let i be the
// interval, m = keep/i the whole intervals kept, and k the grid index of the instant the pass reads,
// so that instant is boundary(k) + f with 0 <= f < i. The partition j intervals back is
// [boundary(k-j), boundary(k-j+1)), and it is wholly expired exactly when its upper bound is at or
// before the cutoff:
//
//	boundary(k-j+1) <= boundary(k) + f - m*i   <=>   (m+1-j)*i <= f
//
// and since 0 <= f < i that holds exactly when j >= m+1. With the hour interval and the six-hour
// keep of theRetainedRetention, m is 6: the six partitions 7 to 12 intervals back are dropped and
// the seven from 0 to 6 survive, the last of them holding the cutoff instant itself.
func TestExactlyTheWhollyExpiredPartitionsAreDroppedAcrossAWideAgeSpread(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	interval := cfg.Retention.PartitionInterval
	before := theClockOf(t, pool)

	var planted, wantDropped, wantKept []Range
	for j := theAgeSpread; j >= 0; j-- {
		ranged := rangeAt(gridIndex(before, interval)-int64(j), interval)
		planted = append(planted, ranged)
		if j >= theWholeIntervalsKept+1 {
			wantDropped = append(wantDropped, ranged)
		} else {
			wantKept = append(wantKept, ranged)
		}
	}
	plantMarkedPartitions(t, pool, planted)
	assertTheArithmeticAgreesWithTheProductionCutoff(t, pool, cfg, before, wantDropped)

	report := oneRetentionPass(t, pool, cfg, Options{})
	stillOnTheSameGridLine(t, before, theClockOf(t, pool), interval)

	for _, ranged := range wantDropped {
		isGone(t, pool, harnessSchema, ranged.Name, "its whole extent is older than now minus keep")
	}
	for _, ranged := range wantKept {
		survives(
			t, pool, harnessSchema, ranged.Name,
			"it holds an instant inside the retention window, so an event in it is still kept",
		)
	}
	if want := (Result{Outcome: OutcomeWorkDone, Dropped: len(wantDropped)}); report.settled != want ||
		report.refused != nil {
		t.Errorf(
			"the pass reported %+v and %v, want %+v and no error",
			report.settled, report.refused, want,
		)
	}
	if report.stats != (statsReading{}) {
		t.Errorf("the pass recorded %+v over a schema every drop of which succeeded", report.stats)
	}
}

// assertTheArithmeticAgreesWithTheProductionCutoff crosses the derivation above against the exported
// decision the pass itself takes, over the partitions as the catalog reports them. Two independent
// statements of one cutoff: a defect in either fails here rather than being ratified by the other.
func assertTheArithmeticAgreesWithTheProductionCutoff(
	t *testing.T, pool *pgxpool.Pool,
	cfg config.Config, at time.Time, wantDropped []Range,
) {
	t.Helper()

	planned := slices.Sorted(slices.Values(namesOf(PlanMaintenance(at, cfg, observedRanges(t, pool)).Drop)))
	if want := slices.Sorted(slices.Values(namesOf(wantDropped))); !slices.Equal(planned, want) {
		t.Fatalf(
			"at %s the planner drops %v and the derivation drops %v; a partition j intervals "+
				"back is wholly expired exactly when j >= %d, because (m+1-j)*i <= f holds for no "+
				"smaller j at any f in [0, i)", at, planned, want, theWholeIntervalsKept+1,
		)
	}
}

// theEqualityTriple is criterion 35: the partition whose upper bound is exactly the cutoff, the one
// an interval before it, and the one an interval after.
//
// The derivation. TO is exclusive (M11), so a partition ending exactly at now-keep holds no instant
// at or after the cutoff and is therefore wholly expired -- it is dropped, and that row is the only
// input separating a `To <= cutoff` guard from a `To < cutoff` one. The partition beginning at the
// cutoff holds the cutoff instant itself, which must still be kept -- it is retained, and that row
// separates `To <= cutoff` from `To <= cutoff + interval`. Neither is implied by the other, and with
// only one of them the guard is two implementations and either could ship.
//
// The cutoff is pinned at an instant this case chose rather than at whichever instant the pass's own
// transaction began, which is what makes "exactly" exact: the plan is computed here, at that
// instant, and handed to the apply. A pass reading its own clock would meet a cutoff a few
// microseconds later, where `<` and `<=` answer alike and the row would discriminate nothing.
func TestThePartitionEndingExactlyAtTheCutoffIsDroppedAndItsSuccessorIsKept(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	interval := cfg.Retention.PartitionInterval
	at := theClockOf(t, pool)
	cutoff := at.Add(-cfg.Retention.Keep)

	earlier := rangesEndingBefore(cutoff, interval, 2) // [cutoff-2i, cutoff-i) and [cutoff-i, cutoff)
	later := rangeEndingAt(cutoff.Add(interval), interval)
	plantMarkedPartitions(t, pool, append(slices.Clone(earlier), later))

	plan := PlanMaintenance(at, cfg, observedRanges(t, pool))
	if want := slices.Sorted(slices.Values(namesOf(earlier))); !slices.Equal(
		slices.Sorted(slices.Values(namesOf(plan.Drop))), want,
	) {
		t.Fatalf(
			"at the cutoff %s the planner drops %v, and the arithmetic drops %v: the range "+
				"ending exactly at the cutoff holds no instant at or after it, and the range beginning "+
				"there holds the cutoff instant itself", cutoff, namesOf(plan.Drop), want,
		)
	}

	report := oneAppliedDropPlan(t, pool, cfg, plan.Drop)

	isGone(
		t, pool, harnessSchema, earlier[1].Name,
		"its upper bound is exactly now minus keep and TO is exclusive, so it holds nothing kept",
	)
	isGone(t, pool, harnessSchema, earlier[0].Name, "it ends one whole interval before the cutoff")
	survives(
		t, pool, harnessSchema, later.Name,
		"it begins at the cutoff and therefore holds the cutoff instant itself",
	)
	if want := (Result{Outcome: OutcomeWorkDone, Dropped: 2}); report.settled != want ||
		report.refused != nil {
		t.Errorf(
			"the pass reported %+v and %v, want %+v and no error",
			report.settled, report.refused, want,
		)
	}
}
