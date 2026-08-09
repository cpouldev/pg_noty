//go:build integration

package schema

import (
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 25 realised on disk, and the ownership marker every partition the pass
// creates has to carry.
//
// Every expected bound here is compared against the value the alignment arithmetic produced, and
// never against the partitions the run created: an expectation read back from a run asserts that
// the implementation still does what it already did, wrong answer included, and then defends that
// answer against every later fix. The bound is read back through pg_get_expr, which is what makes
// the comparison mechanical rather than inferred from where rows happened to land.

// partitionsUnderTheDefaults is the eight partitions criterion 25 names. It is asserted below as
// the k-range's own consequence -- floor(now/24h) through floor((now+168h)/24h) -- and written down
// here only so a reader meets the number the criterion writes. It is not pinned for any other
// configuration, and the row that derives it fails first if the defaults move.
const partitionsUnderTheDefaults = 8

// aConvergedEventLog is an event log one pass has already converged: the partitions the arithmetic
// asks for, plus the permanent DEFAULT one. It answers the ranges alongside the pool, because every
// case built on it derives its expectations from those rather than from what it finds on disk.
func aConvergedEventLog(t *testing.T) (*pgxpool.Pool, config.Config, []Range) {
	t.Helper()

	pool, cfg := aMaintainedEventLog(t)
	before := theClockOf(t, pool)
	report := onePass(t, pool, cfg, Options{})
	wanted := theHorizonBetween(t, before, theClockOf(t, pool))

	if want := (Result{Outcome: OutcomeWorkDone, Created: len(wanted)}); report.settled != want ||
		report.refused != nil {
		t.Fatalf(
			"the pass that was to converge the event log reported %+v and %v, want %+v and no "+
				"error", report.settled, report.refused, want,
		)
	}
	return pool, cfg, wanted
}

// TestOnePassCreatesExactlyTheHorizonTheAlignmentArithmeticDerives is criterion 25.
//
// The count is asserted as a consequence of the k-range and not as a literal: 168h is seven whole
// 24h intervals, and adding a whole number of intervals moves the floor by exactly that many, so
// last = first + 7 and the set holds eight ranges. Both halves are recomputed from the
// configuration, so a configuration with another precreate is answered by the arithmetic rather
// than by this number.
func TestOnePassCreatesExactlyTheHorizonTheAlignmentArithmeticDerives(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aMaintainedEventLog(t)
	before := theClockOf(t, pool)

	report := onePass(t, pool, cfg, Options{})
	wanted := theHorizonBetween(t, before, theClockOf(t, pool))

	interval, precreate := cfg.Retention.PartitionInterval, cfg.Retention.Precreate
	if wholeIntervalsAhead := int(precreate / interval); len(wanted) != wholeIntervalsAhead+1 {
		t.Fatalf(
			"the arithmetic asks for %d ranges where k runs over %d whole %s intervals ahead "+
				"plus the one holding now", len(wanted), wholeIntervalsAhead, interval,
		)
	}
	if len(wanted) != partitionsUnderTheDefaults {
		t.Fatalf(
			"the %s/%s defaults ask for %d ranges, and criterion 25 names %d",
			interval, precreate, len(wanted), partitionsUnderTheDefaults,
		)
	}

	if want := (Result{Outcome: OutcomeWorkDone, Created: len(wanted)}); report.settled != want {
		t.Errorf("the pass reported %+v, want %+v", report.settled, want)
	}
	assertEveryBoundIsTheArithmetics(t, pool, wanted, interval)
}

// assertEveryBoundIsTheArithmetics reads each partition's bound back through pg_get_expr and
// compares it against the range the arithmetic asked for, position by position -- so two partitions
// sharing one extent cannot satisfy the row named for a third.
//
// The span is asserted separately from the two bounds, because a pair of partitions each shifted by
// the same amount would span one interval apiece and still cover the wrong week.
func assertEveryBoundIsTheArithmetics(
	t *testing.T, pool *pgxpool.Pool, wanted []Range,
	interval time.Duration,
) {
	t.Helper()

	for i, ranged := range wanted {
		written := renderedBoundOf(t, pool, ranged.Name, nil)
		observed, fault := rangeFrom(written, ranged.Name)
		if fault != boundOK {
			t.Errorf("partition %d of the horizon is bounded %s, which %s", i, written, fault)
			continue
		}
		if !observed.From.Equal(ranged.From) || !observed.To.Equal(ranged.To) {
			t.Errorf(
				"partition %d is bounded [%s, %s), and the arithmetic asks for [%s, %s)",
				i, observed.From, observed.To, ranged.From, ranged.To,
			)
		}
		if spans := observed.To.Sub(observed.From); spans != interval {
			t.Errorf("partition %d spans %s, want exactly one %s interval", i, spans, interval)
		}
	}

	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the partitions the pass created: %v", err)
	}
	if len(found.Bounded) != len(wanted) {
		t.Errorf(
			"the event log holds %d bounded partitions %v and the arithmetic asks for %d %v",
			len(found.Bounded), namesOf(found.Bounded), len(wanted), namesOf(wanted),
		)
	}
	if found.Default != PartitionDefault {
		t.Errorf(
			"the DEFAULT partition reads as %q after a pass that created %d partitions, want %q",
			found.Default, len(wanted), PartitionDefault,
		)
	}
}

// TestEveryPartitionThePassCreatesCarriesTheOwnershipMarker is the mechanism Step 14's first drop
// guard reads, asserted through catalog.go's own marker read rather than through the comment text:
// an unmarked partition is one retention refuses to drop for as long as it exists, so a pass that
// created eight partitions and marked seven leaves one growing forever.
func TestEveryPartitionThePassCreatesCarriesTheOwnershipMarker(t *testing.T) {
	skipIfShort(t)

	pool, _, wanted := aConvergedEventLog(t)

	if len(wanted) == 0 {
		t.Fatal("the pass created no partition, so this case would report every one of them marked")
	}
	for _, ranged := range wanted {
		assertOwnedByThisInstance(t, pool, harnessSchema, ranged.Name)
	}
}
