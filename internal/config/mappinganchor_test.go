package config

import (
	"slices"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// Where a diagnostic about a whole mapping points, asked of the derivation directly rather than
// through a stage. What the two scopes do with the answer -- and the merge arrangements that made
// the derivation more than `Values[0]` -- are missingkeyanchor_test.go's.

// TestTheMappingAnchorIsItsFirstKeyRatherThanItsOwnToken pins ADR-6's class for a
// mapping-level condition. A mapping's own token is the colon of its first entry, so
// anchoring on it would put every mapping-level caret on punctuation.
func TestTheMappingAnchorIsItsFirstKeyRatherThanItsOwnToken(t *testing.T) {
	src, node := nodeAt(t, "database:\n  schema: noty\n", "$.database")

	got := positionOfMapping(src, node)

	// Line 2 is `  schema: noty`: two spaces, so `schema` begins at rune column 3.
	if got.Line() != 2 || got.Col() != 3 {
		t.Errorf("positionOfMapping(...) = %d:%d, want 2:3, the first key", got.Line(), got.Col())
	}
	// The locator stays the mapping's, because the diagnostic is about the mapping.
	if got.Path() != "database" {
		t.Errorf("Path() = %q, want the mapping's own locator %q", got.Path(), "database")
	}
	// Without this the test could pass on a mapping whose own token already was the
	// first key, and the correction it exists to prove would be invisible.
	if own := positionOf(src, node).Col(); own == got.Col() {
		t.Errorf("the mapping's own token is already at column %d; ADR-6's correction is no longer observable here", own)
	}
}

// TestTheMappingAnchorFallsBackWhenThereIsNoFirstKey covers the shapes ADR-6's class
// does not apply to: a non-mapping node has no first key, so it anchors on its own token.
func TestTheMappingAnchorFallsBackWhenThereIsNoFirstKey(t *testing.T) {
	src, node := nodeAt(t, "listeners:\n  - name: a\n", "$.listeners")

	got := positionOfMapping(src, node)

	// Line 2 is `  - name: a`: the sequence's own token is the `-` at rune column 3.
	if got.Line() != 2 || got.Col() != 3 {
		t.Errorf("positionOfMapping(sequence) = %d:%d, want 2:3, the node's own token", got.Line(), got.Col())
	}
}

// TestTheMappingAnchorReDerivesTheOrderRatherThanReadingTheSlice guarantees the order is re-derived
// after a rewrite rather than read off the slice, and no document reaches it:
// stage E appends the entries it adds, so a mapping's own entries are still in the order they were
// written and the slice happens to answer correctly.
//
// That is exactly why it is constructed here. The claim is that the *next* pass to rewrite a
// mapping's entries cannot move this caret, and a claim about a rewrite nobody has written yet can
// only be asserted by performing one. Both entries below belong to this mapping, so the ownership
// filter keeps both and only the ordering can decide between them.
func TestTheMappingAnchorReDerivesTheOrderRatherThanReadingTheSlice(t *testing.T) {
	src, node := nodeAt(t, "database:\n  schema: noty\n  listen_url: x\n", "$.database")

	mapping, isMapping := node.(*ast.MappingNode)
	if !isMapping {
		t.Fatalf("node is %T, want a mapping", node)
	}
	slices.Reverse(mapping.Values)

	got := positionOfMapping(src, mapping)

	// Line 2 is `  schema: noty`, still the first key the document writes, though it is now last in
	// the slice. Line 3 is `  listen_url: x`, which is what reading the slice would answer.
	if got.Line() != 2 || got.Col() != 3 {
		t.Errorf("positionOfMapping(...) = %d:%d, want 2:3 -- the first key written, not the first held",
			got.Line(), got.Col())
	}
}

// TestTheMappingAnchorClaimsAnEntryTheFoldGaveAnElementLocator reaches the second of the two ways a
// locator extends its parent's. A hand-written mapping reaches its entries through `.`, and every
// mapping a diagnostic is currently anchored on is one -- but the operations fold gives each entry
// it builds the element locator its list form had (listsugar.go), so `$.listeners[0].operations`
// reaches `$.listeners[0].operations[0]` through `[` instead.
//
// Nothing anchors on that mapping today: the two scopes are the document root and a listener. The
// arm is asserted rather than dropped because dropping it would leave the helper quietly wrong
// about a shape this package itself builds, and the next caller would inherit the fallback with no
// test to say so.
func TestTheMappingAnchorClaimsAnEntryTheFoldGaveAnElementLocator(t *testing.T) {
	src, root, faults := stageE(t, operationsOf("    operations: [insert, update]\n"), corpusVariables)
	if len(faults) != 0 {
		t.Fatalf("the fixture does not reach stage F: %q", messagesOf(faults))
	}

	folded := nodeIn(t, root, "$.listeners[0].operations")

	got := positionOfMapping(src, folded)

	// Line 7 is `    operations: [insert, update]`: four spaces, so `operations` begins at rune 5,
	// the `[` the fold takes its own token from is at rune 17 and `insert` at rune 18.
	if got.Line() != 7 || got.Col() != 18 {
		t.Errorf("positionOfMapping(folded operations) = %d:%d, want 7:18 -- the first operation",
			got.Line(), got.Col())
	}
	// Without this the case could pass on a fold whose own token already was its first entry, and
	// the arm it exists to reach would be invisible.
	if own := positionOf(src, folded).Col(); own == got.Col() {
		t.Errorf("the folded mapping's own token is already at column %d; this case reaches no arm", own)
	}
}
