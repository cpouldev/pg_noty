package source

import (
	"testing"
	"time"
)

// pairOf writes one sample in milliseconds, the unit every derivation below is done in. A
// millisecond is 1e6 nanoseconds and every figure here is under 2^53, so a quotient of two pairs
// written this way is bit-identical to the same quotient of the two plain integers -- which is what
// lets each row carry its arithmetic instead of a number copied out of a run.
func pairOf(baseline, triggered int) overheadSample {
	return overheadSample{
		baseline:  time.Duration(baseline) * time.Millisecond,
		triggered: time.Duration(triggered) * time.Millisecond,
	}
}

// TestTheOverheadVerdictAnswersOnlyWhatItMeasured pins the population the verdict rests on. A median
// over an empty or two-element run of pairs is a number that discards nothing, and the run that
// produced no verdict must be told apart from the run that produced a ratio inside the ceiling.
func TestTheOverheadVerdictAnswersOnlyWhatItMeasured(t *testing.T) {
	quiet := pairOf(900, 1200)
	for _, testCase := range []struct {
		name         string
		samples      []overheadSample
		wantPairs    int
		wantMeasured bool
	}{
		{name: "no pair completed"},
		{name: "one pair, which is its own median", samples: []overheadSample{quiet}, wantPairs: 1},
		{name: "two pairs, the count at which a median discards nothing",
			samples: []overheadSample{quiet, quiet}, wantPairs: 2},
		{name: "three pairs, the fewest a verdict may rest on",
			samples: []overheadSample{quiet, quiet, quiet}, wantPairs: 3, wantMeasured: true},
		{name: "a pair whose baseline arm never started is no quotient",
			samples: []overheadSample{quiet, quiet, quiet, {triggered: time.Second}}, wantPairs: 3,
			wantMeasured: true},
		{name: "a pair whose triggered arm never started is no quotient",
			samples: []overheadSample{quiet, quiet, {baseline: time.Second}}, wantPairs: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			verdict := judgeOverhead(testCase.samples)
			if verdict.pairs != testCase.wantPairs || verdict.measured != testCase.wantMeasured {
				t.Fatalf("judgeOverhead reported pairs=%d measured=%t, want pairs=%d measured=%t; the "+
					"population is the pairs a quotient can be taken of, and %d of them is the floor",
					verdict.pairs, verdict.measured, testCase.wantPairs, testCase.wantMeasured,
					overheadLeastPairs)
			}
			// Fail closed: the figure an unmeasured verdict carries must be one the ceiling
			// refuses, so that deleting the live test's skip branch fails the run instead of
			// passing it on a ratio nothing measured.
			if !verdict.measured && !overheadExceedsTheCeiling(verdict.ratio) {
				t.Fatalf("an unmeasured verdict carries ratio %v, which the ceiling admits; a run "+
					"that measured nothing would read as one that passed", verdict.ratio)
			}
		})
	}
}

// TestTheMedianRatioSurvivesContentionAndRefusesARealRegression is the substance of the ceiling
// under the statistic that replaced the maximum. Each row carries the arithmetic producing its
// expected ratio and names the rival statistic it rules out, and the ratio is asserted by equality
// beside the ceiling's own verdict, so a statistic reaching the same pass or fail by the wrong route
// still fails the row.
func TestTheMedianRatioSurvivesContentionAndRefusesARealRegression(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		samples     []overheadSample
		wantRatio   float64
		wantRefusal bool
	}{
		{
			// The run that failed at 2.002. Its log reported sibling ratios of 0.974, 1.409 and
			// 2.002, and finding B recorded its baseline arm ranging 853 ms to 7.58 s: 7383/7580 =
			// 0.974, 1409/1000 = 1.409 and 1708/853 = 2.002 reproduce the three it published, and
			// 1300/900 = 1.444 is a fourth, quiet pair. Sorted, the two middle ratios are 1.409 and
			// 1.444, so the median is their mean. Rules out the MAXIMUM, which is 2.002 and refuses.
			name: "the contended run whose largest pair ratio exceeded the ceiling",
			samples: []overheadSample{
				pairOf(7580, 7383), pairOf(1000, 1409), pairOf(853, 1708), pairOf(900, 1300),
			},
			wantRatio: (float64(1409)/float64(1000) + float64(1300)/float64(900)) / 2,
		},
		{
			// A real run of the finished code, transcribed to whole milliseconds from the log of the
			// invocation that decided this statistic. Its baseline arm found one window at 674 ms
			// against 879, 893 and 950 ms, while its triggered arm held 1230-1245 ms throughout.
			// Sorted, the middle two ratios are 1245/893 and 1238/879. Rules out the QUOTIENT OF
			// EACH ARM'S MINIMA, which is 1230/674 = 1.825 -- the same verdict, a different number,
			// and the widest-spread of the six candidates over 24 such invocations.
			name:      "a run whose baseline arm alone found a rare quiet window",
			samples:   []overheadSample{pairOf(950, 1230), pairOf(879, 1238), pairOf(893, 1245), pairOf(674, 1240)},
			wantRatio: (float64(1245)/float64(893) + float64(1238)/float64(879)) / 2,
		},
		{
			// A trigger genuinely costing 2.2x on a machine that loaded a different arm in each of
			// the first two pairs: pair 1's triggered arm (3000 rather than 2.2 x 850 = 1870) and
			// pair 2's baseline (5000 rather than 850). Sorted the ratios are 0.374, 2.2, 2.2 and
			// 3.529, so both middle values are 2.2 and the median is 2.2 exactly. Rules out the
			// MINIMUM, which is 1870/5000 = 0.374 and admits, and the QUOTIENT OF THE ARMS' MEANS,
			// which is 2180/1900 = 1.147 and admits.
			name: "a 2.2x regression with one arm contended in each of two pairs",
			samples: []overheadSample{
				pairOf(850, 3000), pairOf(5000, 1870), pairOf(850, 1870), pairOf(900, 1980),
			},
			wantRatio:   float64(1870) / float64(850),
			wantRefusal: true,
		},
		{
			// The same regression over three pairs, which is the odd-count branch of the median and
			// the floor a verdict may rest on. Sorted the ratios are 1870/850 = 2.200,
			// 1950/880 = 2.216 and 2050/900 = 2.278, so the median is the middle one and not an
			// extreme of the three.
			name:        "a 2.2x regression over the fewest pairs a verdict may rest on",
			samples:     []overheadSample{pairOf(850, 1870), pairOf(900, 2050), pairOf(880, 1950)},
			wantRatio:   float64(1950) / float64(880),
			wantRefusal: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			verdict := judgeOverhead(testCase.samples)
			if !verdict.measured {
				t.Fatalf("judgeOverhead measured nothing from %d complete pairs", len(testCase.samples))
			}
			if verdict.ratio != testCase.wantRatio {
				t.Errorf("median ratio = %.17g, want %.17g; the verdict is the median of the pairs' "+
					"own ratios", verdict.ratio, testCase.wantRatio)
			}
			if got := overheadExceedsTheCeiling(verdict.ratio); got != testCase.wantRefusal {
				t.Errorf("the ceiling refused %.3f = %t, want %t", verdict.ratio, got, testCase.wantRefusal)
			}
		})
	}
}
