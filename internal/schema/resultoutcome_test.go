package schema

import (
	"slices"
	"testing"
)

// This file holds criterion 33's vocabulary half. Step 4's Expected Output names stats_test.go for
// both the counters and Result; the two are written across two files, and that is this step's one
// deviation from it -- taken for the reason Step 3 declared the same one, that a single file would
// breach the 200-line budget this package's own gate enforces. Nothing else moved.

// TestTheOutcomeSetIsExactlyTheThreeCriterion33Names quantifies over the declared set rather than
// over a list copied into one assertion, so a fourth outcome has to join it.
func TestTheOutcomeSetIsExactlyTheThreeCriterion33Names(t *testing.T) {
	if len(Outcomes) != 3 {
		t.Fatalf("%d outcomes are declared %v; criterion 33 names three -- nothing was needed, work "+
			"was done, work was attempted and failed", len(Outcomes), Outcomes)
	}

	for i, outcome := range Outcomes {
		if outcome == "" {
			t.Errorf("outcome %d is empty, so it is indistinguishable from a Result nobody filled in", i)
		}
		if slices.Contains(Outcomes[i+1:], outcome) {
			t.Errorf("outcome %d is %q, which a later member spells identically", i, outcome)
		}
	}
}

// theDistinguishableResults are the three passes criterion 33 requires a caller to tell apart. The
// first two are the pair that matters: both did nothing, and only the reason differs.
var theDistinguishableResults = []struct {
	name   string
	result Result
}{
	{name: "a pass that did nothing because nothing was needed",
		result: Result{Outcome: OutcomeNothingNeeded}},
	{name: "a pass that did nothing because it failed",
		result: Result{Outcome: OutcomeFailed}},
	{name: "a pass that created two partitions and dropped one",
		result: Result{Outcome: OutcomeWorkDone, Created: 2, Dropped: 1}},
}

// TestAPassThatDidNothingIsNotAPassThatFailed is criterion 33's own sentence, asserted by
// inequality. Both results carry zero created and zero dropped, so only the outcome separates them:
// a Result that folded the two into "no work" would pass every other row in this file.
func TestAPassThatDidNothingIsNotAPassThatFailed(t *testing.T) {
	nothingNeeded := Result{Outcome: OutcomeNothingNeeded}
	failed := Result{Outcome: OutcomeFailed}

	if nothingNeeded.Created != 0 || nothingNeeded.Dropped != 0 ||
		failed.Created != 0 || failed.Dropped != 0 {
		t.Fatalf("the pair under test issued DDL (%+v, %+v); the claim is about two passes that both "+
			"did nothing", nothingNeeded, failed)
	}
	if nothingNeeded == failed {
		t.Error("a pass that did nothing because nothing was needed equals one that did nothing " +
			"because it failed, so criterion 33 has no subject in Steps 11 and 14")
	}
}

// TestEachOutcomeIsDistinguishableFromEveryOther crosses the three against each other, so "work done"
// is asserted distinct from both of the do-nothing pair rather than only from the one a pairwise
// check happened to name.
func TestEachOutcomeIsDistinguishableFromEveryOther(t *testing.T) {
	for i, first := range theDistinguishableResults {
		for _, second := range theDistinguishableResults[i+1:] {
			if first.result == second.result {
				t.Errorf("%s and %s are the same value %+v", first.name, second.name, first.result)
			}
		}
	}
}

// TestAResultNobodyFilledInClaimsNoOutcome is the fail-closed row. The zero value has to be outside
// the declared set: a forgotten return path must not be able to report that nothing was needed, which
// is the one outcome a caller acts on by doing nothing at all.
func TestAResultNobodyFilledInClaimsNoOutcome(t *testing.T) {
	if outcome := (Result{}).Outcome; slices.Contains(Outcomes, outcome) {
		t.Errorf("the zero Result reports %q, which is a declared outcome; a path that forgot to set "+
			"one would be read as a deliberate report", outcome)
	}
}
