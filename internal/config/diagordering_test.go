package config

import (
	"testing"
)

// TestPermutingRuleCannotChangeTheResult puts every step of the survivor preference
// under permutation rather than only the Rule tiebreak in isolation. The set carries
// three competing duplicate groups: one decided by Path, one by a remediation hint, and
// one -- the hard case -- by Rule alone, where a different entry genuinely survives each
// way round and the result is identical only because those entries agree on every field
// a renderer reads.
func TestPermutingRuleCannotChangeTheResult(t *testing.T) {
	base := Errors{
		// Two listeners aliasing one broken anchor: the smaller Path survives.
		{Rule: R36, File: "a.yaml", Line: 9, Col: 12, Path: "listeners[1].destination.url", Msg: "bad scheme"},
		{Rule: R37, File: "a.yaml", Line: 9, Col: 12, Path: "listeners[0].destination.url", Msg: "bad scheme"},
		// One report says how to fix it: the hint-carrying entry survives.
		{Rule: R41, File: "a.yaml", Line: 14, Col: 7, Path: "listeners[0].payload.colums", Msg: "unknown key"},
		{Rule: R32, File: "a.yaml", Line: 14, Col: 7, Path: "listeners[0].payload.colums", Msg: "unknown key", Hint: `did you mean "columns"?`},
		// Identical in everything a renderer reads: only Rule can decide.
		{Rule: R28, File: "a.yaml", Line: 21, Col: 3, Path: "listeners[0].operations", Msg: "unknown operation"},
		{Rule: R29, File: "a.yaml", Line: 21, Col: 3, Path: "listeners[0].operations", Msg: "unknown operation"},
		// Singletons in two files, so ordering has work to do as well.
		{Rule: R1, File: "b.yaml", Line: 4, Col: 2, Path: "worker.batch_size", Msg: "too small"},
		{Rule: W1, File: "a.yaml", Line: 2, Col: 5, Path: "version", Msg: "must equal 1"},
	}
	permuted := make(Errors, len(base))
	copy(permuted, base)
	rules := []RuleID{R2, R19, R40, R7, W2, R11, R23, R5}
	for i := range permuted {
		permuted[i].Rule = rules[i]
	}

	want := fingerprint(base.normalized())
	if got := fingerprint(permuted.normalized()); got != want {
		t.Errorf("permuting Rule changed the result:\ngot\n%s\nwant\n%s", got, want)
	}

	// The third group is what makes the equality above worth asserting, so it is checked
	// to still be one: line 21 must collapse to a single diagnostic, and the rule that
	// survives must differ between the two runs. Edit the set so those entries stop
	// colliding and this fails, instead of the invariance quietly becoming vacuous.
	if forward, backward := survivorOf(t, base, 21), survivorOf(t, permuted, 21); forward == backward {
		t.Errorf("the same rule %q survived both permutations; the Rule tiebreak was not exercised", forward)
	}
}

// survivorOf returns the rule of the single diagnostic normalization kept at a line.
func survivorOf(t *testing.T, diags Errors, line int) RuleID {
	t.Helper()

	var found []Error
	for _, diag := range diags.normalized() {
		if diag.Line == line {
			found = append(found, diag)
		}
	}
	if len(found) != 1 {
		t.Fatalf("normalized() kept %d diagnostics on line %d, want exactly 1", len(found), line)
	}
	return found[0].Rule
}

// TestOrderIsTotalSoInputOrderCannotSurvive proves the ordering leaves nothing to
// input order: after de-duplication no two diagnostics compare equal, so the same
// set produces the same sequence no matter which order the rules reported it in.
func TestOrderIsTotalSoInputOrderCannotSurvive(t *testing.T) {
	// Every pair here shares as much of the key as a de-duplicated set allows: the
	// same position with different paths, and the same position and path with
	// different messages.
	set := Errors{
		{Rule: R1, File: "a.yaml", Line: 2, Col: 3, Path: "p", Msg: "second"},
		{Rule: R2, File: "a.yaml", Line: 2, Col: 3, Path: "p", Msg: "first"},
		{Rule: R3, File: "a.yaml", Line: 2, Col: 3, Path: "a", Msg: "first"},
		{Rule: R4, File: "a.yaml", Line: 2, Col: 9, Path: "p", Msg: "first"},
	}

	forward := fingerprint(set.normalized())

	reversed := make(Errors, len(set))
	for i, diag := range set {
		reversed[len(set)-1-i] = diag
	}
	if backward := fingerprint(reversed.normalized()); backward != forward {
		t.Errorf("reversing the input changed the output:\ngot\n%s\nwant\n%s", backward, forward)
	}

	ordered := set.normalized()
	for i := 1; i < len(ordered); i++ {
		if compareByPosition(ordered[i-1], ordered[i]) == 0 {
			t.Errorf("diagnostics %d and %d compare equal, so their order depends on input order", i-1, i)
		}
	}
}

// TestDeduplicationKeepsTheHintCarryingDuplicate pins the preference between two
// reports of one complaint: the report that says how to fix it survives, whichever
// order the two arrived in.
func TestDeduplicationKeepsTheHintCarryingDuplicate(t *testing.T) {
	withHint := Error{Rule: R41, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "boom", Hint: `did you mean "columns"?`}
	withoutHint := Error{Rule: R41, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "boom"}

	tests := []struct {
		name  string
		input Errors
	}{
		{name: "the hint arrives second", input: Errors{withoutHint, withHint}},
		{name: "the hint arrives first", input: Errors{withHint, withoutHint}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.input.normalized()

			if len(got) != 1 {
				t.Fatalf("normalized() kept %d diagnostics, want 1: %+v", len(got), got)
			}
			if got[0].Hint != withHint.Hint {
				t.Errorf("surviving Hint = %q, want %q", got[0].Hint, withHint.Hint)
			}
		})
	}
}

// TestTheSurvivingDuplicateNamesTheSameRuleWhateverTheInputOrder closes the last
// order dependence in de-duplication. Two diagnostics that agree on everything a
// renderer reads and differ only in Rule must collapse to the same one either way
// round, because Step 13 inventories fixture coverage by Rule.
func TestTheSurvivingDuplicateNamesTheSameRuleWhateverTheInputOrder(t *testing.T) {
	pair := Errors{
		{Rule: R41, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "boom"},
		{Rule: R28, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "boom"},
	}
	reversed := Errors{pair[1], pair[0]}

	forward, backward := pair.normalized(), reversed.normalized()

	if len(forward) != 1 || len(backward) != 1 {
		t.Fatalf("normalized() kept %d and %d diagnostics, want 1 each", len(forward), len(backward))
	}
	if forward[0].Rule != backward[0].Rule {
		t.Errorf("the surviving rule is %q one way round and %q the other", forward[0].Rule, backward[0].Rule)
	}
	if forward[0].Rule != R28 {
		t.Errorf("surviving Rule = %q, want %q, the smaller of the two", forward[0].Rule, R28)
	}
}
