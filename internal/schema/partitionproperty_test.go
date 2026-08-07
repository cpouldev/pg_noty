package schema

import (
	"testing"
	"time"
)

// The generated dimension is the one the invariants are quantified over: the instant, the interval
// and the horizon, spanning both sides of the Unix epoch. The bounds below keep a triple inside what
// the representation expresses and what a test can enumerate: a 1ns interval with an unbounded
// horizon would require 10^15 ranges.
const (
	// maxGeneratedIntervalNanos is about 11.5 days.
	maxGeneratedIntervalNanos = 1_000_000_000_000_000
	// maxGeneratedIntervalsAhead keeps the horizon within 64 intervals plus a partial one, so the
	// required set stays enumerable at every interval.
	maxGeneratedIntervalsAhead = 64
	// maxGeneratedNowNanos is about 146 years either side of the epoch: 1824 to 2116.
	maxGeneratedNowNanos = 1 << 62
)

// generatedSeed is one committed seed, named for the boundary class it reaches. Seeds are raw
// generated values rather than table positions, so a seed keeps its meaning when a table
// grows.
type generatedSeed struct {
	name                                               string
	nowNanos, intervalNanos, intervalCount, extraNanos int64
}

// 1785196800s after the epoch is 2026-07-28T00:00:00Z (20662 whole days); 21600s more is 06:00.
const (
	theDayLineSeconds = 1785196800
	theSixthHour      = theDayLineSeconds + 21600
	oneDayNanos       = 86400 * 1_000_000_000
)

var theGeneratedSeeds = []generatedSeed{
	{name: "a present-day instant on a daily grid", nowNanos: theSixthHour * 1_000_000_000,
		intervalNanos: oneDayNanos},
	{name: "an instant six hours before the Unix epoch", nowNanos: -21600 * 1_000_000_000,
		intervalNanos: oneDayNanos, intervalCount: 1},
	{name: "the epoch itself", nowNanos: 0, intervalNanos: oneDayNanos},
	{name: "the horizon exactly on a grid line", nowNanos: theSixthHour * 1_000_000_000,
		intervalNanos: oneDayNanos, extraNanos: 64800 * 1_000_000_000},
	{name: "a precreate that is not a whole multiple of the interval",
		nowNanos: theSixthHour * 1_000_000_000, intervalNanos: oneDayNanos,
		extraNanos: 21600 * 1_000_000_000},
	{name: "the smallest interval internal/config permits", nowNanos: theSixthHour * 1_000_000_000},
	{name: "an instant before the epoch at the smallest interval", nowNanos: -1},
	{name: "now exactly on a grid line", nowNanos: theDayLineSeconds * 1_000_000_000,
		intervalNanos: oneDayNanos},
	{name: "the widest interval and horizon generated", nowNanos: theSixthHour * 1_000_000_000,
		intervalNanos: maxGeneratedIntervalNanos - 1, intervalCount: maxGeneratedIntervalsAhead - 1},
}

// normalisedSeed turns four generated int64s into a triple internal/config could have produced: an
// interval greater than zero (R10) and a precreate of at least one whole interval (R12). Both the
// fuzz targets and the seed-coverage test read it, so a class the test says is covered is the class
// the target actually runs.
func normalisedSeed(nowNanos, intervalNanos, intervalCount, extraNanos int64) (
	time.Time, time.Duration, time.Duration) {
	interval := magnitudeUnder(intervalNanos, maxGeneratedIntervalNanos)
	if interval == 0 {
		interval = 1
	}
	whole := 1 + magnitudeUnder(intervalCount, maxGeneratedIntervalsAhead)
	precreate := time.Duration(whole)*time.Duration(interval) +
		time.Duration(magnitudeUnder(extraNanos, interval))

	return time.Unix(0, nowNanos%maxGeneratedNowNanos), time.Duration(interval), precreate
}

// magnitudeUnder folds a generated value into [0, limit) without letting a negative one escape.
func magnitudeUnder(raw, limit int64) int64 {
	bounded := raw % limit
	if bounded < 0 {
		return -bounded
	}
	return bounded
}

// FuzzTheRequiredSetTilesTheEpochGrid quantifies AC 24 over the generated domain: whatever the
// instant, the interval and the horizon, the ranges tile contiguously on exact epoch multiples and
// hold now and the horizon under the half-open rule.
func FuzzTheRequiredSetTilesTheEpochGrid(f *testing.F) {
	for _, seed := range theGeneratedSeeds {
		f.Add(seed.nowNanos, seed.intervalNanos, seed.intervalCount, seed.extraNanos)
	}

	f.Fuzz(func(t *testing.T, nowNanos, intervalNanos, intervalCount, extraNanos int64) {
		now, interval, precreate := normalisedSeed(nowNanos, intervalNanos, intervalCount, extraNanos)

		got := RequiredRanges(now, retentionOf(interval, precreate, precreate))

		assertGridShape(t, got, now, interval, precreate)
		if shortfall := CoverageShortfall(got, now, precreate); shortfall != 0 {
			t.Errorf("the required set for %s at %s leaves a shortfall of %s",
				precreate, now.UTC().Format(time.RFC3339Nano), shortfall)
		}
	})
}

// FuzzARangeNameIsInjectiveAndWithinTheIdentifierLimit quantifies AC 32 over the same domain. The
// third candidate is the same instant at a different interval, which is the pair a name derived
// from the start alone collides.
func FuzzARangeNameIsInjectiveAndWithinTheIdentifierLimit(f *testing.F) {
	for _, seed := range theGeneratedSeeds {
		f.Add(seed.nowNanos, seed.intervalNanos, seed.intervalCount, seed.extraNanos)
	}

	f.Fuzz(func(t *testing.T, nowNanos, intervalNanos, intervalCount, extraNanos int64) {
		now, interval, precreate := normalisedSeed(nowNanos, intervalNanos, intervalCount, extraNanos)
		other := interval + 1

		assertNamingIsInjective(t, []Range{
			rangeAt(gridIndex(now, interval), interval),
			rangeAt(gridIndex(now.Add(precreate), interval), interval),
			rangeAt(gridIndex(now, other), other),
		})
	})
}

// assertNamingIsInjective asserts both directions: two ranges of one extent must share a name, and
// two of different extents must not. A one-directional check passes for a constant name.
func assertNamingIsInjective(t *testing.T, candidates []Range) {
	t.Helper()

	for i, first := range candidates {
		assertWithinTheIdentifierLimit(t, first.Name)
		for _, second := range candidates[i+1:] {
			sameExtent := first.From.Equal(second.From) && first.To.Equal(second.To)
			if sameExtent != (first.Name == second.Name) {
				t.Errorf("%s is named %q and %s is named %q", boundsOf([]Range{first}), first.Name,
					boundsOf([]Range{second}), second.Name)
			}
		}
	}
}

// theBoundaryClasses are the classes this file's code branches on. A generator that never produces
// one of them reads as universal while excluding exactly the case the branch was written for.
var theBoundaryClasses = []struct {
	name  string
	holds func(now time.Time, interval, precreate time.Duration) bool
}{
	{name: "an instant before the Unix epoch", holds: func(now time.Time, _, _ time.Duration) bool {
		return now.UnixNano() < 0
	}},
	{name: "the epoch itself", holds: func(now time.Time, _, _ time.Duration) bool {
		return now.UnixNano() == 0
	}},
	{name: "now exactly on a grid line",
		holds: func(now time.Time, interval, _ time.Duration) bool {
			return now.UnixNano()%int64(interval) == 0
		}},
	{name: "the horizon exactly on a grid line",
		holds: func(now time.Time, interval, precreate time.Duration) bool {
			return now.Add(precreate).UnixNano()%int64(interval) == 0
		}},
	{name: "a precreate that is not a whole multiple of the interval",
		holds: func(_ time.Time, interval, precreate time.Duration) bool {
			return precreate%interval != 0
		}},
	{name: "the smallest interval internal/config permits",
		holds: func(_ time.Time, interval, _ time.Duration) bool {
			return interval == time.Nanosecond
		}},
}

// TestTheSeedCorpusCoversEveryBoundaryClassTheCodeBranchesOn keeps the committed corpus honest: a
// seed pruned or retargeted later must fail here rather than silently narrowing both targets.
func TestTheSeedCorpusCoversEveryBoundaryClassTheCodeBranchesOn(t *testing.T) {
	if len(theBoundaryClasses) != 6 {
		t.Fatalf("%d boundary classes are declared; update this count with the set",
			len(theBoundaryClasses))
	}
	if len(theGeneratedSeeds) == 0 {
		t.Fatal("the seed corpus is empty, so every class below would be reported unreached")
	}

	for _, class := range theBoundaryClasses {
		if !seedReaching(class.holds) {
			t.Errorf("no committed seed normalises into %q, so neither fuzz target enters it "+
				"on a default run", class.name)
		}
	}
}

// seedReaching reports whether any committed seed normalises into a class.
func seedReaching(holds func(now time.Time, interval, precreate time.Duration) bool) bool {
	for _, seed := range theGeneratedSeeds {
		if holds(normalisedSeed(seed.nowNanos, seed.intervalNanos, seed.intervalCount,
			seed.extraNanos)) {
			return true
		}
	}
	return false
}
