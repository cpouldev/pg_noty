package config

import (
	"path/filepath"
	"testing"
)

// The corpus this stage adds, and the derivation of every diagnostic in it.
//
// Each expectation below is counted from the fixture on disk rather than recorded from a run, and
// the count is written into the row so a reader can check a caret without running anything. That
// is what makes the `.golden` files beside them trustworthy: the rule, line, column, message and
// hint of every block are pinned here, and the layout those pieces are arranged in is pinned
// independently of any fixture by TestRenderReproducesTheSpecificationsBlockByteForByte and
// TestCaretAndHintFollowThePrefixWidth.

// stageFDiagnostic is one diagnostic a fixture must produce.
type stageFDiagnostic struct {
	rule RuleID
	line int
	col  int
	// msg is the whole message, and hint the whole hint. Both are written out rather than matched
	// by substring, because the golden beside the fixture reproduces them exactly and a partial
	// expectation would let the two drift.
	msg  string
	hint string
}

// stageFCorpus is every fixture this step adds to `testdata/invalid`, with the diagnostics it must
// produce in the order the boundary sorts them -- by line, since no two share one.
var stageFCorpus = map[string][]stageFDiagnostic{
	// Three comment lines, then `version` on 4. Each stray sits at the first non-space rune of
	// its own line: rune 1 at the root, 3 one level in, 5 two, 7 three, 9 four.
	"R41_ten_unknown_keys": {
		{rule: R41, line: 5, col: 1, msg: `unknown field "rootstray"`},
		{rule: R41, line: 8, col: 3, msg: `unknown field "dbstray"`},
		{rule: R41, line: 10, col: 3, msg: `unknown field "workerstray"`},
		{rule: R41, line: 12, col: 3, msg: `unknown field "retentionstray"`},
		{rule: R41, line: 15, col: 5, msg: `unknown field "retrystray"`},
		{rule: R41, line: 19, col: 5, msg: `unknown field "listenerstray"`},
		{rule: R41, line: 22, col: 9, msg: `unknown field "updatestray"`},
		{rule: R41, line: 24, col: 7, msg: `unknown field "payloadstray"`},
		{rule: R41, line: 27, col: 7, msg: `unknown field "destinationstray"`},
		{rule: R41, line: 30, col: 9, msg: `unknown field "signingstray"`},
	},

	// The listener list is written flush with its key, so `payload` sits at rune 3 and its keys
	// at rune 5 -- which is the layout the Diagnostic rendering convention's own example uses.
	"R41_did_you_mean_columns": {
		{rule: R41, line: 14, col: 5, msg: `unknown field "colums"`, hint: `did you mean "columns"?`},
	},

	// `frobnicate` is eight edits from `mode`, the nearest declared payload key, so no candidate
	// is within the threshold and the diagnostic carries no hint at all.
	"R41_no_suggestion_for_an_unrecognisable_name": {
		{rule: R41, line: 13, col: 7, msg: `unknown field "frobnicate"`},
	},

	// The four keys the contract moved. `secret` is one edit from `secrets`, so this fixture is
	// also where the precedence is visible: the hint names the new shape rather than the
	// neighbouring spelling.
	"R41_keys_the_contract_moved": {
		{
			rule: R41, line: 15, col: 5, msg: `unknown field "when"`,
			hint: "move it under operations.<op>.when, so it applies to the statement it was written for",
		},
		{
			rule: R41, line: 16, col: 5, msg: `unknown field "columns"`,
			hint: "move it under operations.update.columns, the only statement a column filter applies to",
		},
		{
			rule: R41, line: 18, col: 7, msg: `unknown field "type"`,
			hint: "HTTP is the only destination kind, so there is nothing left to choose; remove the key",
		},
		{
			rule: R41, line: 21, col: 9, msg: `unknown field "secret"`,
			hint: "use signing.secrets, a list, so a secret can be rotated without downtime",
		},
	},

	// Both names begin at rune 5; the second repeats the first, written on line 8.
	"R20_header_names_differing_only_in_case": {
		{
			rule: R20, line: 9, col: 5,
			msg:  `header "x-trace-id" differs only in case from the one on line 8`,
			hint: "HTTP field names are case-insensitive, so the two would be one header on the wire",
		},
	},

	// A key nobody wrote has no token, so the caret is the document root's first key: `version`
	// on line 3, rune 1, after two comment lines.
	"R22_listeners_absent": {
		{rule: R22, line: 3, col: 1, msg: `missing required key "listeners"`},
	},
	// The same, with `database` as the root's first key because `version` is the missing one.
	"R1_version_absent": {
		{rule: R1, line: 3, col: 1, msg: `missing required key "version"`},
	},
	// AC #30: one diagnostic naming the key inside the absent block, anchored on the root's
	// first key, `version` on line 3.
	"R3_database_absent": {
		{rule: R3, line: 3, col: 1, msg: `missing required key "database.url"`},
	},

	// `    operations: {}` -- four spaces, eleven characters of `operations:`, a space, so the
	// empty mapping's own `{` is at rune 17.
	"R27_operations_empty": {
		{rule: R27, line: 9, col: 17, msg: `"operations" must hold at least one entry`},
	},
	// Six spaces, so `truncate` begins at rune 7.
	"R28_unsupported_operation": {
		{
			rule: R28, line: 10, col: 7, msg: `unknown field "truncate"`,
			hint: "only insert, update and delete are supported",
		},
	},
	// Eight spaces, so `columns` begins at rune 9.
	"R29_columns_under_insert": {
		{rule: R29, line: 11, col: 9, msg: `"columns" is legal only under "update"`},
	},
	"R29_columns_under_delete": {
		{rule: R29, line: 10, col: 9, msg: `"columns" is legal only under "update"`},
	},
	// `        columns: []` -- eight spaces, seven characters of `columns`, a colon and a space,
	// so the empty list's `[` is at rune 18.
	"R29_columns_empty_under_update": {
		{rule: R29, line: 11, col: 18, msg: `"columns" must hold at least one entry`},
	},

	// A listener is the scope its own missing keys answer to, and its first key is whichever the
	// fixture still writes: `  - table` and `  - name` both put it at rune 5 of line 6.
	"R23_listener_name_absent": {
		{rule: R23, line: 6, col: 5, msg: `missing required key "name"`},
	},
	"R26_listener_table_absent": {
		{rule: R26, line: 6, col: 5, msg: `missing required key "table"`},
	},
	"R36_destination_url_absent": {
		{rule: R36, line: 7, col: 5, msg: `missing required key "destination.url"`},
	},

	// `    operations: insert` -- the value begins at rune 17, where `{` sits in the empty-mapping
	// fixture above.
	"shape_operations_as_a_bare_scalar": {
		{
			rule: RuleShape, line: 8, col: 17, msg: `"operations" must be a mapping or a list`,
			hint: bothOperationForms,
		},
	},

	// The parser detects this one while parsing, so the run ends at stage B and the wording is
	// the parser's own. Six spaces, so the repeated key is at rune 7 and the first is at [10:7].
	"R42_operation_written_twice": {
		{rule: R42, line: 11, col: 7, msg: `mapping key "insert" already defined at [10:7]`},
	},
}

// TestEveryFixtureThisStepAddsProducesTheDiagnosticsItWasWrittenFor is what makes the goldens
// beside these fixtures assertions rather than recordings.
func TestEveryFixtureThisStepAddsProducesTheDiagnosticsItWasWrittenFor(t *testing.T) {
	if len(stageFCorpus) == 0 {
		t.Fatal("the corpus is empty, so this test would pass vacuously")
	}

	for name, want := range stageFCorpus {
		t.Run(name, func(t *testing.T) {
			path := fixture(name)
			_, _, got := Parse(readFixtureBytes(t, path), filepath.Base(path), corpusEnvironment())

			if len(got) != len(want) {
				t.Fatalf("%d diagnostics, want %d: %q", len(got), len(want), messagesOf(got))
			}
			for i, expected := range want {
				assertDiagnosticMatches(t, got[i], expected)
			}
		})
	}
}

func assertDiagnosticMatches(t *testing.T, got Error, want stageFDiagnostic) {
	t.Helper()

	if got.Rule != want.rule {
		t.Errorf("Rule = %q, want %q", got.Rule, want.rule)
	}
	if got.Line != want.line || got.Col != want.col {
		t.Errorf("%s is at %d:%d, want %d:%d", got.Msg, got.Line, got.Col, want.line, want.col)
	}
	if got.Msg != want.msg {
		t.Errorf("Msg = %q, want %q", got.Msg, want.msg)
	}
	if got.Hint != want.hint {
		t.Errorf("Hint = %q, want %q", got.Hint, want.hint)
	}
}
