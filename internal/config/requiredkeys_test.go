package config

import "testing"

// Where a diagnostic about a key nobody wrote points, and the two conflations the contract forbids.
//
// Every expected line and column below is counted from the fixture written beside it rather than
// recorded from a run. A mapping's own token is the colon of its first entry -- measured, and
// pinned by TestGoccyGivesAMappingTheColonOfItsFirstEntry -- so a caret that landed on punctuation
// would be the sign that the anchor came from the mapping rather than from its first key.

// The documents these cases are cut from, written out so a reader can count a column without
// running anything. Both begin at line 1 with the root mapping's first key at rune 1.
const (
	// 1 version, 2 database, 3 url, 4 listeners.
	rootWithEverything = "version: 1\n" +
		"database:\n" +
		"  url: postgres://noty:pw@db.internal:5432/noty\n" +
		"listeners: []\n"

	// The same through line 4, then the listener: 5 `  - name: order_paid`, 6 table,
	// 7 operations, 8 destination, 9 url. Two spaces, a dash and a space precede `name`, so the
	// listener mapping's first key begins at rune 5.
	listenerLine   = 5
	listenerColumn = 5
)

// theRootsFirstKey is where every top-level required key's caret belongs: line 1, rune 1, because
// each document below opens with the root mapping's first key.
const (
	rootLine   = 1
	rootColumn = 1
)

// TestAMissingRequiredKeyIsAnchoredOnItsScopesFirstKey is ADR-6's class for a key with no token of
// its own, over all seven the contract requires (CK-5). The four top-level ones answer to the
// document root and the three-plus-one per-listener ones to the listener that lacks them, which is
// the division schema.go's two scopes encode.
func TestAMissingRequiredKeyIsAnchoredOnItsScopesFirstKey(t *testing.T) {
	for _, tc := range requiredKeyAnchorCases() {
		t.Run(tc.name, func(t *testing.T) {
			diags, decodable := stageF(t, tc.document)

			if len(diags) != 1 {
				t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
			}
			if diags[0].Rule != tc.wantRule {
				t.Errorf("Rule = %q, want %q", diags[0].Rule, tc.wantRule)
			}
			if want := missingRequiredKey(tc.wantKey).message; diags[0].Msg != want {
				t.Errorf("Msg = %q, want %q", diags[0].Msg, want)
			}
			if diags[0].Line != tc.wantLine || diags[0].Col != tc.wantCol {
				t.Errorf("anchored at %d:%d, want %d:%d -- the enclosing scope's first key token",
					diags[0].Line, diags[0].Col, tc.wantLine, tc.wantCol)
			}
			if !decodable {
				t.Error("a missing key stopped the run; only a shape the later stages cannot read may")
			}
		})
	}
}
