package config

import (
	"testing"
)

// Stage F's error policy -- accumulate, and stop only on a shape the stages after it cannot read
// -- and the exclusion ADR-1 makes of the library's own strictness.
//
// It is named for the stage's own function rather than for its letter, as normalizepolicy_test.go
// is, because a file called stagefpolicy_test.go would sit one letter from two others making
// different claims about different stages.

// TestStageFAccumulatesEveryShapeMistakeInOneRun is the requirement ADR-1 exists for, over a
// document whose mistakes are of four different kinds at four different depths. A walk that
// returned at its first finding would report one of them, and one is what the library's own
// strict decode gives (V4).
func TestStageFAccumulatesEveryShapeMistakeInOneRun(t *testing.T) {
	// 1 rootstray, 2 database, 3 url, 4 defaults, 5 headers, 6 X-Trace, 7 x-trace, 8 listeners,
	// 9 the listener's name, 10 its table, 11 operations, 12 a stray inside it, 13 destination,
	// 14 its url. `version` is written nowhere, which is the fourth kind.
	document := `rootstray: 1
database:
  url: postgres://noty:pw@db.internal:5432/noty
defaults:
  headers:
    X-Trace: a
    x-trace: b
listeners:
  - name: order_paid
    table: public.orders
    operations:
      insert:
        listenerstray: 1
    destination:
      url: https://hooks.example.test/order-paid
`

	diags, decodable := stageF(t, document)

	found := make(map[RuleID]int, 4)
	for _, diag := range diags {
		found[diag.Rule]++
	}
	want := map[RuleID]int{R41: 2, R20: 1, R1: 1}

	if len(diags) != 4 {
		t.Fatalf("%d diagnostics, want 4 -- two unknown keys, one header collision and one missing key: %q",
			len(diags), messagesOf(diags))
	}
	for rule, count := range want {
		if found[rule] != count {
			t.Errorf("%d diagnostics under %q, want %d", found[rule], rule, count)
		}
	}
	if !decodable {
		t.Error("unknown keys, a header collision and a missing key stopped the run; none of them may")
	}
}

// TestOnlyAShapeTheLaterStagesCannotReadStopsTheRun is the stop condition stated as an
// enumeration rather than as one example.
//
// The condition is not "a wrong node kind" alone, and the difference is deliberate: a mapping
// holding one key twice is the other member, because `NodeToValue` refuses such a mapping
// outright and the run after this one would fail with no position at all
// (keyrepeats_test.go measures both halves of that). Everything else this stage finds leaves a
// document the later stages can still judge, which is what AC #12 needs.
func TestOnlyAShapeTheLaterStagesCannotReadStopsTheRun(t *testing.T) {
	tests := []struct {
		name          string
		document      string
		wantDecodable bool
	}{
		{
			name:          "an unknown key",
			document:      listenerHolding("    listenerstray: 1\n"),
			wantDecodable: true,
		},
		{
			name:          "a missing required key",
			document:      aListenerOf(requiredName, requiredTable, requiredDestination),
			wantDecodable: true,
		},
		{
			name:          "an empty container",
			document:      operationsOf("    operations: {}\n"),
			wantDecodable: true,
		},
		{
			name:          "a key written in the wrong place",
			document:      operationsOf("    operations:\n      insert:\n        columns: [status]\n"),
			wantDecodable: true,
		},
		{
			name:          "two header names differing only in case",
			document:      destinationOf("    destination:\n      url: https://h.test/x\n      headers:\n        X-T: a\n        x-t: b\n"),
			wantDecodable: true,
		},
		{
			name:          "a value of the wrong shape",
			document:      "version: 1\ndatabase: text\nlisteners: []\n",
			wantDecodable: false,
		},
		{
			name:          "one key written in two spellings",
			document:      twoRenderingsOfOneHeaderName,
			wantDecodable: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags, decodable := stageF(t, tc.document)

			if len(diags) == 0 {
				t.Fatalf("the document produced no diagnostics, so this row asserts nothing about stopping")
			}
			if decodable != tc.wantDecodable {
				t.Errorf("decodable = %t, want %t", decodable, tc.wantDecodable)
			}
		})
	}
}

// strictnessPinTests is the V4 pins, which are the only functions in the package that may pass a
// strictness option: one error per call, no promise about *which* error, and blindness inside a
// NodeUnmarshaler subtree.
//
// The list is deliberately of test functions and holds no helper. A helper applying Strict would be
// callable from every other file of the package, which is the hole this assertion exists to close, so the
// three below each apply it in their own body rather than sharing one call.
var strictnessPinTests = []string{
	"TestGoccyStrictDecodeDoesNotPromiseWhichUnknownFieldItReports",
	"TestGoccyStrictDecodeIsBlindInsideAnUnmarshaler",
	"TestGoccyStrictDecodeSurfacesOneErrorPerCall",
}
