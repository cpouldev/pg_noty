package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The golden-file harness, hand-rolled per skill Pattern 10: a glob, a byte comparison and
// an opt-in flag, with no third-party dependency.
//
// Every `.golden` file in this corpus was written by hand from its fixture and the
// Diagnostic rendering convention, never generated and then declared correct. The
// derivations are recorded beside the conditions they belong to in
// TestTheCorpusCoversEveryStructuralCondition, so a reader can check a caret column without
// running anything.

// updateGoldens is opt-in and off by default, so regenerating the corpus is always a
// deliberate act. Before using it on a format change, check the rendered output by hand:
// the value of a golden file is that it disagrees with the code.
var updateGoldens = flag.Bool(goldenUpdateFlag, false, "rewrite the .golden files from the current rendered output")

// goldenUpdateFlag is the flag's name, and updateCommand the command that records a golden. Both are
// spelled once, because the message below is the only instruction a contributor adding a fixture ever
// sees and a message naming a flag the harness does not register teaches the wrong command.
const (
	goldenUpdateFlag = "update"
	updateCommand    = "go test ./internal/config/ -run Golden -" + goldenUpdateFlag
)

// missingGoldenMessage is what a contributor adding a fixture is told. It is a value rather than a
// t.Fatalf call site, so it can be asserted without failing a test to read it: the mechanism formats
// the message and compareGolden decides to fail with it
// (TestAMissingGoldenNamesTheCommandThatCreatesIt).
func missingGoldenMessage(golden, got string) string {
	return golden + " does not exist. Check this output by hand against the Diagnostic rendering " +
		"convention, then create it with `" + updateCommand + "`:\n" + got
}

var (
	invalidCorpus    = filepath.Join("testdata", "invalid")
	validCorpus      = filepath.Join("testdata", "valid")
	unreadableCorpus = filepath.Join("testdata", "unreadable")
)

const (
	fixtureExtension = ".yaml"
	goldenExtension  = ".golden"
)

// TestInvalidFixturesRenderTheirGoldens is the corpus every later step adds to: each
// fixture's rendered diagnostics must equal its golden byte for byte.
func TestInvalidFixturesRenderTheirGoldens(t *testing.T) {
	if covered := renderCorpus(t); len(covered) == 0 {
		t.Fatal("the corpus is empty, so this test would pass vacuously")
	}
}

// TestARunWithoutUpdateNeverWritesAGolden proves the gate rather than assuming it: the
// whole corpus is rendered and compared between two readings of every golden's modification
// time, so a write on the default path would show up here as a changed timestamp.
func TestARunWithoutUpdateNeverWritesAGolden(t *testing.T) {
	if *updateGoldens {
		t.Skip("-update is set, so writing the goldens is the point of this run")
	}
	before := goldenTimestamps(t)

	renderCorpus(t)

	after := goldenTimestamps(t)
	if len(before) == 0 {
		t.Fatal("no golden files found, so this test would pass vacuously")
	}
	for path, was := range before {
		if now, still := after[path]; !still || !now.Equal(was) {
			t.Errorf("%s was written during a run without -update: %v then %v", path, was, now)
		}
	}
}

// TestTheCorpusCoversEveryStructuralCondition ties the corpus to AC #14's six conditions,
// so a fixture cannot quietly go missing. Each row records the derivation of its golden:
// the prefix is 3 + the digits of the widest line number + 3, and the caret sits that many
// columns plus the diagnostic's own column from the left.
func TestTheCorpusCoversEveryStructuralCondition(t *testing.T) {
	tests := []struct {
		condition string
		fixture   string
		wantRule  RuleID
	}{
		// Stage A. Neither has a token, so neither golden holds a snippet or a caret.
		{condition: "nonexistent path", fixture: absentFixture(), wantRule: RuleRead},
		{condition: "unreadable path", fixture: directoryFixture(), wantRule: RuleRead},

		// Stage C. Both shapes of "this file configures nothing" are positionless too.
		{condition: "empty file", fixture: fixture("empty"), wantRule: RuleDocument},
		{condition: "comment-only file", fixture: fixture("comment_only"), wantRule: RuleDocument},

		// Line 1 is `- name: order_paid`, and the sequence's token is the `-` at rune 1.
		// One digit, so the prefix is 7 and the caret sits at column 8.
		{condition: "non-mapping root", fixture: fixture("sequence_root"), wantRule: RuleDocument},

		// Line 4 is `version: 1`, the second document's first key at rune 1. The block
		// spans lines 2 to 4, still one digit, so the caret is at column 8.
		{condition: "two documents", fixture: fixture("two_documents"), wantRule: RuleDocument},

		// Line 3 is `  schema: other`, so the repeated key is at rune 3 and the caret at
		// 7 + 3 = 10.
		{condition: "duplicate mapping key", fixture: fixture("duplicate_key"), wantRule: R42},
	}

	for _, tc := range tests {
		t.Run(tc.condition, func(t *testing.T) {
			diags := diagnosticsOf(t, tc.fixture)

			if len(diags) != 1 {
				t.Fatalf("%s produced %d diagnostics, want exactly 1: %+v", tc.fixture, len(diags), diags)
			}
			if diags[0].Rule != tc.wantRule {
				t.Errorf("Rule = %q, want %q", diags[0].Rule, tc.wantRule)
			}
			if _, err := os.Stat(goldenFor(tc.fixture)); err != nil {
				t.Errorf("no golden for %s: %v", tc.fixture, err)
			}
		})
	}
}

// TestTheTabIndentationGoldenPointsPastTheCharacterItNames records the one caret in this
// corpus that is known rather than correct, so the next reader does not adopt it as the
// general rule. The parser reports a tab used as indentation as a whitespace token at
// column 1; under its zero-width-tab accounting the tab and the `s` after it share that
// column, and the derivation answers the last of them. The message therefore names the tab
// while the caret sits on the `s` after it (Step 1, Implementation Note 10). Do not "fix"
// the renderer to move it: telling this case apart means branching on the text of an
// invalid token.
func TestTheTabIndentationGoldenPointsPastTheCharacterItNames(t *testing.T) {
	golden := readGolden(t, goldenFor(fixture("tab_indentation")))

	if target := caretTarget(t, golden); target != 's' {
		t.Errorf("caret points at %q, want the %q of schema -- one past the tab the message names", target, 's')
	}
	if !strings.Contains(golden, "found character '\t' that cannot start any token") {
		t.Errorf("golden does not carry the parser's own wording:\n%s", golden)
	}
}

// corpusVariables is the environment the valid corpus is written against: every variable its
// fixtures reference, set to a plausible value.
//
// The valid corpus exists to say "this file loads clean", and since stage D refuses a
// reference to an unset variable, clean is only meaningful against a stated environment.
// A fixture that adds a reference without adding it here fails by that variable's own name,
// which is the failure a contributor can act on.
//
// It is a map rather than only a lookup because a test running the stages directly supplies
// variables that way, and one declaration is what keeps a fixture's environment the same
// whichever entry point reads it.
var corpusVariables = map[string]string{
	"DATABASE_URL":        "postgres://noty:pw@db.internal:5432/noty",
	"DATABASE_LISTEN_URL": "postgres://noty:pw@db.internal:5432/noty",
	"DATABASE_DIRECT_URL": "postgres://noty:pw@direct.internal:5432/noty",
	"ORDER_WEBHOOK_URL":   "https://hooks.example.test/order-paid",
	// The reference the merged anchor carries, so one anchor exercises interpolation, precedence
	// and sharing at once. Its value is a duration because retry.initial_interval is one, which is
	// what keeps the fixture valid for the step that will validate it.
	"RETRY_INITIAL_INTERVAL": "10s",
	"SIGNING_SECRET":         "secret-from-the-environment",
	"SIGNING_SECRET_OLD":     "old-secret-from-the-environment",
	// AC #5's first case: a reference that has to reach an integer field. Its value is text, because
	// every substituted value is, which is the whole point of the case.
	"WORKER_CONCURRENCY": "16",
	// AC #4's value, shared with the fixture written against it so neither can drift.
	"ADVERSARIAL_VALUE": adversarialValue,
}

func corpusEnvironment() EnvLookup {
	return MapEnv(corpusVariables)
}
