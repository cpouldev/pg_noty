package source

import (
	"math"
	"slices"
	"testing"
	"time"
)

// overheadSample is one complete pair of the ceiling's workload: the same 10,000-row transaction
// timed against the untriggered arm and against the triggered one, next to each other in one
// process against one server. The pair is the unit because a quotient of two timings taken on
// different machines, or minutes apart on one, is not a ratio.
type overheadSample struct{ baseline, triggered time.Duration }

// ratio is one pair's own quotient, and the only spelling of it in this package: the per-pair figure
// a run logs and the verdict's own answer are this expression applied to different samples.
func (sample overheadSample) ratio() float64 {
	return float64(sample.triggered) / float64(sample.baseline)
}

// overheadLeastPairs is the fewest complete pairs a verdict may rest on. Three, because the verdict
// is a median and a median of two is the mean of both: at two pairs the outlier the statistic exists
// to discard is counted in full, which is the whole of what it was chosen over. Three is the
// smallest count at which the median discards anything.
const overheadLeastPairs = 3

// overheadExceedsTheCeiling is the whole of the overhead verdict. The live measurement and the
// boundary rows in TestTheOverheadCeilingAdmitsExactlyTwoAndRefusesTheNextValueAbove call this one
// function rather than two spellings of one comparison. 1.7 is written as a literal because the
// specification commits it and this file does not: it is a regression ceiling, not a performance
// target, sitting deliberately far above the 20-30% the design claims, and the structural
// fail-closed assertion remains the primary defence. The comparison is `>` and not `>=` because
// the ceiling admits "at most 1.7x", so a ratio of exactly 1.7 passes.
func overheadExceedsTheCeiling(ratio float64) bool { return ratio > 1.7 }

// overheadVerdict is what a run's samples say about the ceiling. measured false means too few
// pairs completed to answer at all, which is a different outcome from a ratio inside the ceiling
// and must never be read as one.
type overheadVerdict struct {
	ratio    float64
	pairs    int
	measured bool
}

// judgeOverhead answers the median of the pairs' own ratios.
//
// Why the median. The noise here is two-sided in the ratio and one-sided in each arm's duration, and
// only the first of those facts bears on the statistic. Contention lengthens whichever arm meets it,
// so a pair whose triggered arm met the load reports a ratio too high and a pair whose baseline arm
// met it reports one too low: one contended run of this test published sibling pair ratios of 0.974,
// 1.409 and 2.002, and a figure below 1.0 is impossible as a statement about overhead. The median of
// four discards a pair from each tail, which is exactly the two shapes that error takes.
//
// Why not the extremes. The maximum -- what this test used to report -- is drawn from whichever pair
// met the busier machine, and it is what took the ceiling red at 2.002 and 3.482. The minimum is the
// same mistake with the sign flipped, and it is the one that would flatter the implementation. The
// quotient of each arm's own smallest timing is the subtler wrong answer, and it was measured rather
// than argued away: over 24 invocations of unchanged code it spread 1.335 to 1.825, the widest of
// the six candidates, because it rests entirely on one observation of the arm with the fatter lower
// tail -- the untriggered baseline, whose timings ranged 674 ms to 1121 ms against the triggered
// arm's 1159 ms to 1517 ms. The median of the same 24 invocations spread 1.315 to 1.441.
//
// What survives. A trigger that genuinely costs 2.2x reports about 2.2 on every pair, so its median
// is about 2.2 whichever pairs are discarded; contention has to corrupt more than half the pairs in
// one direction to move it, and the tails it does corrupt are the ones thrown away.
// TestTheMedianRatioSurvivesContentionAndRefusesARealRegression drives both directions, and the
// recorded mutation drives it against a real trigger made 2.2x slow.
func judgeOverhead(samples []overheadSample) overheadVerdict {
	usable := usableOverheadPairs(samples)
	if len(usable) < overheadLeastPairs {
		// Fail closed rather than tidily: the live test reads measured first and skips, so this
		// figure is never published, but if that branch were deleted a zero is a ratio the ceiling
		// admits and the run would pass having measured nothing, where +Inf fails.
		return overheadVerdict{ratio: math.Inf(1), pairs: len(usable)}
	}
	return overheadVerdict{ratio: medianOfPairRatios(usable), pairs: len(usable), measured: true}
}

// usableOverheadPairs drops every pair no quotient can be taken of. A zero duration is not a
// measurement, and a zero baseline answers +Inf or -- with a zero triggered arm beside it -- NaN,
// which every comparison against the ceiling answers false: a run that measured nothing would
// pass.
func usableOverheadPairs(samples []overheadSample) []overheadSample {
	usable := make([]overheadSample, 0, len(samples))
	for _, sample := range samples {
		if sample.baseline > 0 && sample.triggered > 0 {
			usable = append(usable, sample)
		}
	}
	return usable
}

// medianOfPairRatios is the middle of the pairs' ratios, and the mean of the two middle ones when
// the count is even. Both branches are reachable: the run asks for four pairs and accepts three.
func medianOfPairRatios(samples []overheadSample) float64 {
	ratios := make([]float64, len(samples))
	for i, sample := range samples {
		ratios[i] = sample.ratio()
	}
	slices.Sort(ratios)

	middle := len(ratios) / 2
	if len(ratios)%2 == 1 {
		return ratios[middle]
	}
	return (ratios[middle-1] + ratios[middle]) / 2
}

// TestTheOverheadCeilingAdmitsExactlyOnePointSevenAndRefusesTheNextValueAbove drives the same predicate the
// live assertion calls. Neither value is reachable by measurement -- no run on healthy hardware arrives at
// the ceiling -- so without these two rows `>` and `>=` are indistinguishable and the ceiling has no
// failing case at all.
func TestTheOverheadCeilingAdmitsExactlyOnePointSevenAndRefusesTheNextValueAbove(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		ratio       float64
		wantRefusal bool
	}{
		// BVA B: the only value separating "at most 1.7x" from "over 1.7x".
		{name: "exactly the committed 1.7 ceiling", ratio: 1.7, wantRefusal: false},
		// BVA B+1: the next representable ratio above 1.7.
		{name: "the next representable ratio above 1.7", ratio: math.Nextafter(1.7, math.Inf(1)),
			wantRefusal: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := overheadExceedsTheCeiling(testCase.ratio); got != testCase.wantRefusal {
				t.Fatalf("overheadExceedsTheCeiling(%.17g) = %t, want %t; the ceiling admits at "+
					"most 1.7x, so 1.7 itself passes and the next value above it does not",
					testCase.ratio, got, testCase.wantRefusal)
			}
		})
	}
}
