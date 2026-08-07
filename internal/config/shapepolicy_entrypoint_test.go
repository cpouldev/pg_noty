package config

import "testing"

// TestTheEntryPointReturnsWhatStageFFound is the accumulation seen from outside the package,
// which is where AC #8's "in one run" is actually claimed. A stage wired so that its findings
// were computed and then dropped would satisfy every assertion above and none of the criterion.
func TestTheEntryPointReturnsWhatStageFFound(t *testing.T) {
	cfg, warnings, errs := Parse([]byte(tenUnknownKeys), "listeners.yaml", corpusEnvironment())

	if len(errs) != 10 {
		t.Fatalf("Parse returned %d diagnostics, want the ten stage F found: %q", len(errs), messagesOf(errs))
	}
	// The document remains readable after stage F and deliberately carries a literal signing
	// secret, so Step 11's later warning layer must accompany the ten shape diagnostics.
	if len(warnings) != 1 || warnings[0].Rule != W1 {
		t.Errorf("Parse returned warnings %+v, want the reachable W1 literal-secret warning", warnings)
	}
	if cfg != nil {
		t.Error("Parse returned a configuration alongside diagnostics")
	}
}

// TestTheEntryPointOrdersStageFsFindingsByPosition is the determinism NFR through the stage that
// produces the most diagnostics per run. The walk visits levels in the order the schema table
// declares them, which is not document order, so the ordering has to be the boundary's rather
// than the walk's.
func TestTheEntryPointOrdersStageFsFindingsByPosition(t *testing.T) {
	_, _, errs := Parse([]byte(tenUnknownKeys), "listeners.yaml", corpusEnvironment())

	if len(errs) < 2 {
		t.Fatalf("%d diagnostics, too few to be out of order", len(errs))
	}
	for i, diag := range errs[1:] {
		if previous := errs[i]; diag.Line < previous.Line {
			t.Errorf("line %d is reported after line %d", diag.Line, previous.Line)
		}
	}
}
