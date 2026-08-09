package config

import (
	"strings"
	"testing"
)

// One mapping naming one thing twice, seen through the two equivalences that decide it: R42's,
// which is what makes two keys the same key, and R20's, which folds the case of an HTTP field
// name. The library measurements the first of them rests on are keyrepeats_test.go's.

// TestTwoRenderingsOfOneKeyAreRefusedWithAPosition is R42's completeness, which is this step's
// answer to the question Implementation Note 17 left open: yes, R42 covers two renderings of one
// value, and stage F is where that is decided because it is the last stage that walks the
// document's own mappings.
func TestTwoRenderingsOfOneKeyAreRefusedWithAPosition(t *testing.T) {
	diags, decodable := stageF(t, twoRenderingsOfOneHeaderName)

	if len(diags) != 1 {
		t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
	}
	if diags[0].Rule != R42 {
		t.Errorf("Rule = %q, want R42", diags[0].Rule)
	}
	// Line 7 is `    0x1: b`: four spaces, so the repeated key begins at rune 5, and the one it
	// repeats is on line 6.
	if diags[0].Line != 7 || diags[0].Col != 5 {
		t.Errorf("anchored at %d:%d, want 7:5, the second of the two spellings", diags[0].Line, diags[0].Col)
	}
	if !strings.Contains(diags[0].Msg, "line 6") {
		t.Errorf("Msg = %q, which does not name the first occurrence's line", diags[0].Msg)
	}
	if decodable {
		t.Error("a mapping holding one key twice is still reported decodable; the decoder cannot read it")
	}
}

// TestANonTextualKeyIsNotAlsoReportedAsARepeatedHeader keeps the two equivalences from
// double-reporting one mistake. Both keys of the fixture above carry no text, so a header rule
// that folded them by name would fold them both onto the empty name and add a second diagnostic
// about a header neither of them is.
func TestANonTextualKeyIsNotAlsoReportedAsARepeatedHeader(t *testing.T) {
	diags, _ := stageF(t, twoRenderingsOfOneHeaderName)

	for _, diag := range diags {
		if diag.Rule == R20 {
			t.Errorf("a key carrying no text was reported as a repeated header: %q", diag.Msg)
		}
	}
}

// TestTwoHeaderNamesDifferingOnlyInCaseAreOneDiagnostic is R20. Two such names would become one
// header on the wire with one of the two values silently discarded, which is why they are refused
// rather than merged here.
func TestTwoHeaderNamesDifferingOnlyInCaseAreOneDiagnostic(t *testing.T) {
	// Lines: 1 version, 2 database, 3 url, 4 defaults, 5 headers, 6 `    X-Trace: a`,
	// 7 `    x-trace: b`. Four spaces, so each name begins at rune 5.
	document := "version: 1\ndatabase:\n  url: postgres://noty:pw@db.internal:5432/noty\n" +
		"defaults:\n  headers:\n    X-Trace: a\n    x-trace: b\nlisteners: []\n"

	diags, decodable := stageF(t, document)

	if len(diags) != 1 {
		t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
	}
	if diags[0].Rule != R20 {
		t.Errorf("Rule = %q, want R20", diags[0].Rule)
	}
	if diags[0].Line != 7 || diags[0].Col != 5 {
		t.Errorf("anchored at %d:%d, want 7:5, the second of the two names", diags[0].Line, diags[0].Col)
	}
	if !strings.Contains(diags[0].Msg, "line 6") {
		t.Errorf("Msg = %q, which does not name the first occurrence's line", diags[0].Msg)
	}
	if !decodable {
		t.Error("a case collision stopped the run; two headers of one name are a delivery mistake, not an unreadable shape")
	}
}

// TestHeaderNamesDifferingBeyondCaseAreAccepted is the other side of the same guard: R20 folds
// case and nothing else, so two genuinely different headers must survive it -- including a pair
// that differs by one character, which a rule comparing names loosely would collapse.
func TestHeaderNamesDifferingBeyondCaseAreAccepted(t *testing.T) {
	document := "version: 1\ndatabase:\n  url: postgres://noty:pw@db.internal:5432/noty\n" +
		"defaults:\n  headers:\n    X-Trace: a\n    X-Traces: b\n    X-Tenant: c\nlisteners: []\n"

	if diags, _ := stageF(t, document); len(diags) != 0 {
		t.Errorf("%d diagnostics for headers that differ by more than case: %q", len(diags), messagesOf(diags))
	}
}
