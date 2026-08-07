package config

import (
	"strings"
	"testing"
)

// The two claims about a missing key's caret that the nine-row anchoring table next door cannot
// make on its own: that the caret is never the mapping's *own* token -- which would still land on
// the right line, so the columns there are not evidence by themselves -- and that an absent block
// is one diagnostic about the key inside it rather than two (AC #30).

// TestAMissingKeysCaretIsNeverOnTheMappingsOwnToken is the negative half of the class, asserted
// rather than implied by the columns above. A block mapping's own token is the colon of its first
// entry, so an implementation that anchored on it would put every required-key caret on
// punctuation -- and would still land on the right *line*, which is what makes the line alone an
// insufficient assertion.
func TestAMissingKeysCaretIsNeverOnTheMappingsOwnToken(t *testing.T) {
	document := aListenerOf(requiredName, requiredTable, requiredOperations)

	src, root, _ := stageE(t, document, corpusVariables)
	listener := nodeIn(t, root, "$.listeners[0]")

	own := positionOf(src, listener)
	viaFirstKey := positionOfMapping(src, listener)

	if own.Col() == viaFirstKey.Col() {
		t.Fatalf("the mapping's own token is already at column %d, so this would assert nothing", own.Col())
	}
	diags, _ := checkShape(src, root)
	if len(diags) != 1 {
		t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
	}
	if diags[0].Col == own.Col() {
		t.Errorf("the caret is at column %d, the mapping's own token; want %d, its first key",
			own.Col(), viaFirstKey.Col())
	}
}

// TestAnAbsentDatabaseBlockIsOneDiagnostic is AC #30's explicit sub-case. `database` is not itself
// required, so the block's absence is reported as the one key inside it that is -- not as a
// complaint about the block and another about its contents.
func TestAnAbsentDatabaseBlockIsOneDiagnostic(t *testing.T) {
	diags, _ := stageF(t, "version: 1\nlisteners: []\n")

	if len(diags) != 1 {
		t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
	}
	if !strings.Contains(diags[0].Msg, "database.url") {
		t.Errorf("Msg = %q, want it to name database.url rather than the block that would hold it", diags[0].Msg)
	}
}

// The two arrangements a merge expansion puts the anchor class in, and the reason they are two.
// Stage E appends the entries a `<<` brings in *after* the ones the author wrote here, and each
// keeps the anchor's own line and locator -- so the slice a scope's first key used to be read out
// of is no longer document order, and holds keys written in a different mapping altogether.
//
// Both listeners below merge one anchor, so both hold an inherited entry from line 5; they differ
// in whether the listener writes a key of its own, which is what makes them the two halves of one
// class rather than one case written twice.
const (
	// 1 version, 2 database, 3 url, 4 `defaults: &shared`, 5 `  timeout: 5s`, 6 listeners,
	// 7 `  - <<: *shared`. The listener writes no key of its own, so once stage E drops the `<<`
	// the only entry left in it was written on line 5, inside `defaults`.
	listenerInheritingEveryKey = "version: 1\n" +
		"database:\n" +
		"  url: postgres://noty:pw@db.internal:5432/noty\n" +
		"defaults: &shared\n" +
		"  timeout: 5s\n" +
		"listeners:\n" +
		"  - <<: *shared\n"

	// The same document with one key written in the listener: 8 `    name: order_paid`, whose
	// `name` begins at rune 5.
	listenerWritingOneKeyAndInheritingTheRest = listenerInheritingEveryKey +
		"    name: order_paid\n"
)

// TestAListenerInheritingEveryKeyAnchorsItsMissingKeysOnItself is the defect the anchor class had
// while it read slice order: a listener written `- <<: *shared` holds nothing but entries the
// anchor's mapping owns, so every one of its four missing-key carets pointed into `defaults`.
//
// Where the caret belongs when the author wrote no key of their own is the listener's own token.
// That is the mapping's `GetToken()`, which every other document anchors *away* from -- but here
// the choice is between punctuation inside the right mapping and a key inside the wrong one, and
// only the first names the listener the author has to fix.
func TestAListenerInheritingEveryKeyAnchorsItsMissingKeysOnItself(t *testing.T) {
	src, root, faults := stageE(t, listenerInheritingEveryKey, corpusVariables)
	if len(faults) != 0 {
		t.Fatalf("the fixture does not reach stage F: %q", messagesOf(faults))
	}

	// Without this the assertion below could pass on a listener that turned out to hold a key of
	// its own after all, which is the arrangement this case exists to rule out.
	if names := namesWrittenIn(t, nodeIn(t, root, "$.listeners[0]")); !equalStrings(names, []string{"timeout"}) {
		t.Fatalf("the expanded listener holds %v; this case needs every key of it to be inherited", names)
	}

	diags, _ := checkShape(src, root)

	if len(diags) != 4 {
		t.Fatalf("%d diagnostics, want the four keys the listener is missing: %q", len(diags), messagesOf(diags))
	}
	for _, diag := range diags {
		// `  - <<: *shared` is line 7, and the listener's own token is the colon at rune 7. Line 5
		// is `  timeout: 5s` inside `defaults` -- the inherited entry, and a caret in another block.
		if diag.Line != 7 || diag.Col != 7 {
			t.Errorf("%q is anchored at %d:%d, want 7:7 -- the listener that lacks it, not the anchor it merges",
				diag.Msg, diag.Line, diag.Col)
		}
	}
}

// TestAListenerWritingOneKeyAnchorsOnItRatherThanOnAnInheritedOne is the other half, and the one
// that rules out the correction going too far. Ordering *every* entry by position would answer the
// inherited line 5 here, because the anchor is written above the listener that merges it -- so the
// entries written elsewhere have to be excluded before the rest are ordered, not merely reordered.
func TestAListenerWritingOneKeyAnchorsOnItRatherThanOnAnInheritedOne(t *testing.T) {
	src, root, faults := stageE(t, listenerWritingOneKeyAndInheritingTheRest, corpusVariables)
	if len(faults) != 0 {
		t.Fatalf("the fixture does not reach stage F: %q", messagesOf(faults))
	}

	// The inherited entry must carry the *earlier* line, or the case cannot tell the two
	// implementations apart.
	inherited := positionOf(src, entryKeyNamed(t, nodeIn(t, root, "$.listeners[0]"), "timeout"))
	written := positionOf(src, entryKeyNamed(t, nodeIn(t, root, "$.listeners[0]"), "name"))
	if inherited.Line() >= written.Line() {
		t.Fatalf("the inherited key is on line %d and the written one on line %d; this case needs the "+
			"inherited one earlier", inherited.Line(), written.Line())
	}

	diags, _ := checkShape(src, root)

	if len(diags) != 3 {
		t.Fatalf("%d diagnostics, want the three keys the listener is missing: %q", len(diags), messagesOf(diags))
	}
	for _, diag := range diags {
		// Line 8 is `    name: order_paid`, whose key begins at rune 5.
		if diag.Line != 8 || diag.Col != 5 {
			t.Errorf("%q is anchored at %d:%d, want 8:5 -- the one key this listener writes",
				diag.Msg, diag.Line, diag.Col)
		}
	}
}
