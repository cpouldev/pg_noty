package schema

import (
	"testing"
	"time"
)

// theRetentionNow and theRetentionKeep put the cutoff on a grid line a reader can check: 72h before
// 2026-07-28T00:00:00Z is 2026-07-25T00:00:00Z.
var (
	theRetentionNow    = utc(2026, 7, 28, 0, 0, 0, 0)
	theRetentionKeep   = 72 * time.Hour
	theRetentionCutoff = utc(2026, 7, 25, 0, 0, 0, 0)
)

// dayRange is the 24h grid range beginning at the given UTC midnight, written out rather than
// computed by the code under test so the corpus states its own bounds.
func dayRange(year int, month time.Month, day int) Range {
	from := utc(year, month, day, 0, 0, 0, 0)
	return Range{From: from, To: from.Add(24 * time.Hour)}
}

// theExpiryRows are AC 34/35's arithmetic half, one verdict per range. The pair that carries the
// dimension is the exact-cutoff row and the one an interval later: neither is implied by the other,
// and together they are what separates a `To <= cutoff` guard from a `To < cutoff` one.
var theExpiryRows = []struct {
	name, discriminates string
	observed            Range
	wantExpired         bool
}{
	{
		name:          "the range whose upper bound is exactly the cutoff",
		discriminates: "To <= now-keep against To < now-keep",
		observed:      dayRange(2026, 7, 24), wantExpired: true,
	},
	{
		name:          "the adjacent range one interval later",
		discriminates: "To <= now-keep against To <= now-keep+interval",
		observed:      dayRange(2026, 7, 25), wantExpired: false,
	},
	{
		name:     "the range one interval earlier",
		observed: dayRange(2026, 7, 23), wantExpired: true,
	},
	{
		name:     "the range holding now",
		observed: dayRange(2026, 7, 28), wantExpired: false,
	},
	{
		name:          "a range ending one nanosecond after the cutoff",
		discriminates: "To <= now-keep against To <= now-keep, at the tightest granularity the representation has",
		observed: Range{From: theRetentionCutoff.Add(-24*time.Hour + 1),
			To: theRetentionCutoff.Add(1)}, wantExpired: false,
	},
	{
		name:        "a range ending one nanosecond before the cutoff",
		observed:    Range{From: theRetentionCutoff.Add(-24*time.Hour - 1), To: theRetentionCutoff.Add(-1)},
		wantExpired: true,
	},
}

// TestExpiredRangesDropAtTheCutoffAndKeepTheRangeAfterIt is AC 34/35. TO is exclusive (M11), so a
// range ending exactly at now-keep holds nothing at or after the cutoff and is expired, and the
// range an interval later holds the cutoff instant itself and is not.
func TestExpiredRangesDropAtTheCutoffAndKeepTheRangeAfterIt(t *testing.T) {
	for _, tc := range theExpiryRows {
		t.Run(tc.name, func(t *testing.T) {
			got := ExpiredRanges([]Range{tc.observed}, theRetentionNow, theRetentionKeep)

			if expired := len(got) == 1; expired != tc.wantExpired {
				t.Errorf("ExpiredRanges returned %s for %s against the cutoff %s, want expired = %t "+
					"(%s)", boundsOf(got), boundsOf([]Range{tc.observed}),
					theRetentionCutoff.Format(time.RFC3339Nano), tc.wantExpired, tc.discriminates)
			}
		})
	}
}

// TestExpiredRangesReturnsOnlyTheExpiredOnesInObservedOrder is the same decision over a whole
// observed set, because a per-range verdict says nothing about what a set returns or in what order:
// retention executes this slice, and an order derived from anything but the catalog's own would
// make a failure attributable to the wrong range.
func TestExpiredRangesReturnsOnlyTheExpiredOnesInObservedOrder(t *testing.T) {
	observed := []Range{
		dayRange(2026, 7, 25), // To 07-26, one interval past the cutoff: kept
		dayRange(2026, 7, 23), // To 07-24, before the cutoff: expired
		dayRange(2026, 7, 28), // holds now: kept
		dayRange(2026, 7, 24), // To 07-25, exactly the cutoff: expired
	}
	want := []Range{dayRange(2026, 7, 23), dayRange(2026, 7, 24)}

	assertRanges(t, ExpiredRanges(observed, theRetentionNow, theRetentionKeep), want)
}

// theCoverageHorizon is 2026-07-28T00:00:00Z + 48h, which is the 2026-07-30 grid line -- so
// "reaching exactly the horizon" is a set of whole partitions rather than a contrived bound.
var (
	theCoverageNow       = utc(2026, 7, 28, 0, 0, 0, 0)
	theCoveragePrecreate = 48 * time.Hour
)

// theCoverageRows are AC 31's triple plus the classes a maximum-of-To reading answers wrongly.
var theCoverageRows = []struct {
	name, discriminates string
	observed            []Range
	want                time.Duration
}{
	{
		name:          "coverage one interval short of the horizon",
		observed:      []Range{dayRange(2026, 7, 28)},
		want:          24 * time.Hour,
		discriminates: "reach >= horizon against reach <= horizon",
	},
	{
		name:          "coverage reaching exactly the horizon",
		observed:      []Range{dayRange(2026, 7, 28), dayRange(2026, 7, 29)},
		want:          0,
		discriminates: "a horizon of now+precreate against one an instant past it",
	},
	{
		name:          "coverage one interval beyond the horizon",
		observed:      []Range{dayRange(2026, 7, 28), dayRange(2026, 7, 29), dayRange(2026, 7, 30)},
		want:          0,
		discriminates: "a clamped shortfall against an unclamped, and so negative, one",
	},
	{
		name:     "no partitions at all",
		observed: nil,
		want:     48 * time.Hour,
	},
	{
		// The far range's To is past the horizon, so a maximum-of-To reading answers zero while a
		// write landing in the missing 07-29 day fails.
		name:     "a hole before the horizon, with coverage resuming past it",
		observed: []Range{dayRange(2026, 7, 28), dayRange(2026, 7, 30)},
		want:     24 * time.Hour,
	},
	{
		name:     "the same coverage observed out of order",
		observed: []Range{dayRange(2026, 7, 29), dayRange(2026, 7, 28)},
		want:     0,
	},
	{
		name:     "a wholly past partition alongside coverage reaching the horizon",
		observed: []Range{dayRange(2026, 7, 26), dayRange(2026, 7, 28), dayRange(2026, 7, 29)},
		want:     0,
	},
}

// TestCoverageShortfallIsZeroAtAndBeyondTheHorizon is AC 31.
//
// What the equality row discriminates is worth stating exactly, because it is not the operator a
// reader expects. Swapping `reach >= horizon` for `reach > horizon` is an equivalent mutation here:
// the fallthrough is horizon.Sub(reach), which is already zero when the two are equal, so no
// formulation of this function can be told apart by that swap. The row is not thereby vacuous -- it
// fails an implementation that requires coverage to reach *past* now+precreate, which is the
// plausible off-by-one on a half-open bound (measured: defining the horizon one nanosecond later
// fails this row and no other row of the retention corpus). The rows that discriminate the two
// operators are its neighbours: short of the horizon, and beyond it.
func TestCoverageShortfallIsZeroAtAndBeyondTheHorizon(t *testing.T) {
	for _, tc := range theCoverageRows {
		t.Run(tc.name, func(t *testing.T) {
			got := CoverageShortfall(tc.observed, theCoverageNow, theCoveragePrecreate)

			if got != tc.want {
				t.Errorf("CoverageShortfall over %s at %s with a %s horizon = %s, want %s (%s)",
					boundsOf(tc.observed), theCoverageNow.Format(time.RFC3339Nano),
					theCoveragePrecreate, got, tc.want, tc.discriminates)
			}
		})
	}
}

// TestTheRequiredSetLeavesNoShortfall ties the two halves of the plan together: the set
// RequiredRanges asks for is exactly the set that answers the coverage check, so a boot that
// created everything the plan named cannot then be refused by criterion 31's own guard.
func TestTheRequiredSetLeavesNoShortfall(t *testing.T) {
	for _, tc := range theHorizonRows {
		t.Run(tc.name, func(t *testing.T) {
			required := RequiredRanges(tc.now, retentionOf(tc.interval, tc.precreate, tc.precreate))

			if got := CoverageShortfall(required, tc.now, tc.precreate); got != 0 {
				t.Errorf("the required set %s still leaves a shortfall of %s",
					boundsOf(required), got)
			}
		})
	}
}
