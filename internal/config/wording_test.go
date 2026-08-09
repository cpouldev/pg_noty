package config

import "testing"

// What every refusal this step owns actually says, pinned by literal.
//
// **Why a literal and not the constant.** Every Msg assertion in the step compared a diagnostic against
// the same variable that holds the wording, so the two could only ever agree: eleven of twelve wordings
// could be replaced with arbitrary text and the whole suite, all 39 goldens included, stayed green. A
// test that reads the value under test is a self-comparison.
//
// mustBeAnElement is the one that matters most: it is AC #33's user-facing element diagnostic, the words
// an author reads when one entry of a list is unusable.
func TestEveryRefusalSaysWhatItIsMeantToSay(t *testing.T) {
	wordings := map[string]struct {
		raised fault
		want   string
	}{
		"a scalar that is not text":       {mustBeText, "expected text"},
		"a scalar that is not an integer": {mustBeAnInteger, "expected an integer"},
		"a scalar that is not a boolean":  {mustBeABoolean, "expected true or false"},
		"a scalar that is not a duration": {
			mustBeADuration,
			"must use a Go duration with units ns, us/µs, ms, s, m or h",
		},
		"a value that is not a list":      {mustBeAList, "expected a list"},
		"a list element that is not text": {mustBeAnElement, "expected a list of scalars"},
		"a value that is not a mapping":   {mustBeAMapping, "expected a mapping"},
		"a list entry that is not one":    {mustBeAnEntryOf, "expected a list of mappings"},
		"an operations value":             {mustBeAnOperationsMapping, "expected a mapping of operation names"},
		"an operation key":                {mustBeAnOperationName, "expected an operation name"},
		"a headers value":                 {mustBeAHeaderMapping, "expected a mapping of header names"},
		"a header key":                    {mustBeAHeaderName, "expected a header name"},
	}

	for name, tc := range wordings {
		t.Run(name, func(t *testing.T) {
			if tc.raised.message != tc.want {
				t.Errorf("the refusal reads %q, want %q", tc.raised.message, tc.want)
			}
		})
	}

	// Pinned so a refusal added later has to be given its words here rather than shipping with whatever
	// it was first written with.
	if len(wordings) != 12 {
		t.Errorf("%d wordings pinned, want the 12 stage G declares", len(wordings))
	}
}
