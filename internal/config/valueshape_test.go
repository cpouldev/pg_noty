package config

import (
	"strings"
	"testing"
)

// What the contract requires of the value beneath a declared key: that its shape is one the key
// accepts, that it holds something where the contract says it must, and that a key beneath the
// wrong sibling is refused before its contents are judged. All four of AC #16's shape cases live
// here; the two that are about a *name* are unknownkey_test.go's and keyrepeats_test.go's.

// TestAValueOfTheWrongShapeIsRefusedAndStopsTheRun covers the pass's one structural refusal. Each
// row writes a value whose YAML shape is not the shape the contract declares for its key, and each
// must be one diagnostic that also tells the caller the document is no longer worth decoding.
func TestAValueOfTheWrongShapeIsRefusedAndStopsTheRun(t *testing.T) {
	tests := []struct {
		name        string
		document    string
		wantMessage string
	}{
		{
			name:        "a scalar where a mapping belongs",
			document:    "version: 1\ndatabase: text\nlisteners: []\n",
			wantMessage: `"database" must be a mapping`,
		},
		{
			name:        "a key written with no value at all",
			document:    "version: 1\ndatabase:\nlisteners: []\n",
			wantMessage: `"database" must be a mapping`,
		},
		{
			name:        "a mapping where a list belongs",
			document:    "version: 1\ndatabase: {url: x}\nlisteners: {}\n",
			wantMessage: `"listeners" must be a list`,
		},
		{
			name:        "a mapping where a scalar belongs",
			document:    "version: {}\ndatabase: {url: x}\nlisteners: []\n",
			wantMessage: `"version" must be a scalar`,
		},
		{
			name:        "a list element of the wrong shape",
			document:    "version: 1\ndatabase: {url: x}\nlisteners: [7]\n",
			wantMessage: `each "listeners" entry must be a mapping`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags, decodable := stageF(t, tc.document)

			if len(diags) != 1 {
				t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
			}
			if diags[0].Msg != tc.wantMessage {
				t.Errorf("Msg = %q, want %q", diags[0].Msg, tc.wantMessage)
			}
			if diags[0].Rule != RuleShape {
				t.Errorf("Rule = %q, want %q", diags[0].Rule, RuleShape)
			}
			if decodable {
				t.Error("the document is still reported decodable; a value of the wrong shape is what stops stage F")
			}
		})
	}
}

// TestABareOperationsScalarShowsBothAcceptedForms is AC #16's bare-scalar case. `operations:
// insert` is neither the map form nor the list sugar, so the diagnostic has to show an author
// both -- which is why the accepted forms are declared beside the key that accepts them rather
// than derived from its shapes.
func TestABareOperationsScalarShowsBothAcceptedForms(t *testing.T) {
	diags, decodable := stageF(t, operationsOf("    operations: insert\n"))

	if len(diags) != 1 {
		t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
	}
	for _, form := range []string{"operations: [insert, update]", "operations: {update: {...}}"} {
		if !strings.Contains(diags[0].Hint, form) {
			t.Errorf("hint %q does not show the accepted form %q", diags[0].Hint, form)
		}
	}
	if decodable {
		t.Error("a value that is neither accepted form is still reported decodable")
	}
}

// TestAnEmptyContainerIsRefusedByTheRuleThatRequiresItToHoldSomething covers both keys the table
// declares an emptiness rule for, and the boundary from the accepting side as well: one entry is
// the smallest legal answer, so a rule written as "more than one" would fail the second row.
func TestAnEmptyContainerIsRefusedByTheRuleThatRequiresItToHoldSomething(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantRule RuleID
		wantPath string
	}{
		{
			name:     "an empty operations mapping",
			body:     "    operations: {}\n",
			wantRule: R27,
			wantPath: "listeners[0].operations",
		},
		{
			name:     "an empty operations list, which stage E folds to an empty mapping",
			body:     "    operations: []\n",
			wantRule: R27,
			wantPath: "listeners[0].operations",
		},
		{
			name:     "an empty column filter under update",
			body:     "    operations:\n      update:\n        columns: []\n",
			wantRule: R29,
			wantPath: "listeners[0].operations.update.columns",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags, _ := stageF(t, operationsOf(tc.body))

			if len(diags) != 1 {
				t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
			}
			if diags[0].Rule != tc.wantRule {
				t.Errorf("Rule = %q, want %q", diags[0].Rule, tc.wantRule)
			}
			if diags[0].Path != tc.wantPath {
				t.Errorf("Path = %q, want %q", diags[0].Path, tc.wantPath)
			}
			if diags[0].Line == 0 || diags[0].Col == 0 {
				t.Errorf("the diagnostic carries position %d:%d, want the container's own token",
					diags[0].Line, diags[0].Col)
			}
		})
	}
}

// TestAContainerHoldingOneThingIsAccepted is the accepting side of the same boundary, from both
// spellings of an operations set, so "at least one" is pinned rather than implied by its
// rejections.
func TestAContainerHoldingOneThingIsAccepted(t *testing.T) {
	accepted := map[string]string{
		"one operation in the map form":  "    operations:\n      insert: {}\n",
		"one operation in the list form": "    operations: [insert]\n",
		"one column under update":        "    operations:\n      update:\n        columns: [status]\n",
	}

	for name, body := range accepted {
		t.Run(name, func(t *testing.T) {
			if diags, _ := stageF(t, operationsOf(body)); len(diags) != 0 {
				t.Errorf("%d diagnostics for a container holding one thing: %q", len(diags), messagesOf(diags))
			}
		})
	}
}
