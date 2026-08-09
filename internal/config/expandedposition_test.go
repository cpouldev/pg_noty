package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// Where a node this stage expanded reports itself. Both halves of the stage build or move nodes
// -- a merge lends one mapping's entry to another, and the fold turns a list element into a key
// and invents a filter beside it -- and a position or a locator lost in either is a diagnostic
// that sends its reader to the wrong line.
//
// It is one file because Implementation Notes 6 and 15 are one subject, and it is the subject
// Steps 5 and 8 have to read before they raise a diagnostic about a merged or folded node.

// inheritedBackoff is where a merged entry reports itself: the anchor's own node, reached through
// the listener that inherited it. The two things a position carries are asserted separately
// because they serve different mechanisms -- the line and column are the de-duplication key, the
// locator is not -- so a failure has to say which one moved.
func inheritedBackoff(t *testing.T) Positioned {
	t.Helper()

	src, root, diags := stageE(t, directKeyBelowTheMergeKey, nil)
	if len(diags) != 0 {
		t.Fatalf("normalizing reported %q", messagesOf(diags))
	}
	return positionOf(src, nodeIn(t, root, "$.retry.backoff"))
}

// TestAMergedEntryKeepsTheAnchorsOwnPosition is what makes ADR-3's de-duplication work: an
// inherited entry is the anchor's very node, so every listener that merged it reports the one
// line, and the diagnostics collapse.
func TestAMergedEntryKeepsTheAnchorsOwnPosition(t *testing.T) {
	if at := inheritedBackoff(t); at.Line() != 3 {
		t.Errorf("the inherited value reports line %d, want line 3 where the anchor writes it", at.Line())
	}
}

// TestAMergedEntryKeepsTheAnchorsOwnPath is what sends a reader to the line they have to edit: a
// diagnostic about an inherited value locates it where the value was written rather than under
// the listener that inherited it (Implementation Note 6, which forbids a later stage re-pathing
// merged nodes).
func TestAMergedEntryKeepsTheAnchorsOwnPath(t *testing.T) {
	if at := inheritedBackoff(t); at.Path() != "base.backoff" {
		t.Errorf("Path = %q, want the anchor's own locator", at.Path())
	}
}

// TestAFoldedOperationKeepsTheListElementsOwnPosition is what keeps a later per-operation
// diagnostic pointing at what the author wrote. The folded key is the element node itself, so
// `operations: [insert, update]` reports its update at the rune the author typed it at.
func TestAFoldedOperationKeepsTheListElementsOwnPosition(t *testing.T) {
	// `operations: [` is thirteen runes, so insert begins at rune 14 and update, eight runes
	// later, at rune 22.
	src, root, _ := stageE(t, "operations: [insert, update]\n", nil)
	columns := map[string]int{"insert": 14, "update": 22}

	folded := valueOfEntry(root, 0).(*ast.MappingNode).Values
	if len(folded) != len(columns) {
		t.Fatalf("the fold produced %d entries, want %d; this would assert nothing", len(folded), len(columns))
	}
	for _, entry := range folded {
		name, _ := keyTextOf(entry.Key)
		at := positionOf(src, entry.Key)

		if at.Line() != 1 || at.Col() != columns[name] {
			t.Errorf("the folded %q is at %d:%d, want 1:%d", name, at.Line(), at.Col(), columns[name])
		}
	}
}

// TestAFoldedOperationsFilterCarriesTheElementsLocator is the other half of the same concern, and
// it is about the node the fold *invents* rather than the one it reuses.
//
// ast.Mapping sets no path, so an empty filter built without one renders its locator as the empty
// string -- and a Step-5 or Step-8 diagnostic anchored on it would name nothing. The element's own
// locator is the honest answer: it is where the author wrote the operation.
func TestAFoldedOperationsFilterCarriesTheElementsLocator(t *testing.T) {
	_, root, _ := stageE(t, "operations: [insert, update]\n", nil)

	folded := valueOfEntry(root, 0).(*ast.MappingNode).Values
	if len(folded) != 2 {
		t.Fatalf("the fold produced %d entries, want 2; this would assert nothing", len(folded))
	}
	for _, entry := range folded {
		if got := entry.Value.GetPath(); got != entry.Key.GetPath() {
			t.Errorf("the filter of %q is at %q, want the element's own %q",
				entry.Key.String(), got, entry.Key.GetPath())
		}
	}
}

// TestTheFoldedOperationsMappingInheritsTheLocatorOfTheValueItReplaces is the question Step 4
// asked of the filter it invented and not of the container it built around it.
//
// The container is `ast.Mapping`'s too, so it also began with a bare BaseNode. Stage F anchors
// R27's "at least one entry" on this very mapping, so an empty locator here is not a cosmetic
// loss: `Path` sits inside the diagnostic sort key and inside prefersOver, and the empty string
// sorts before every real locator -- so the diagnostic would name nothing *and* jump the queue.
//
// It is closed over every spelling the fold accepts, because the two rows that reach the list
// through an alias inherit a *different* locator -- the anchor's own, which Implementation Note 6
// forbids a later stage re-pathing -- and a claim written against the in-place spellings alone
// would read as universal while excluding them.
func TestTheFoldedOperationsMappingInheritsTheLocatorOfTheValueItReplaces(t *testing.T) {
	for spelling, written := range operationsListSpellings {
		t.Run(spelling, func(t *testing.T) {
			if written.locator == "" {
				t.Fatal("this spelling declares no expected locator, so the row would assert nothing")
			}
			root := normalized(t, written.written([]string{"insert"}))

			// The alias spellings write the list under a key of its own first, so the folded
			// operations entry is the document's last rather than its first.
			if got := lastValueOf(t, root).GetPath(); got != written.locator {
				t.Errorf("the folded mapping is at %q, want %q", got, written.locator)
			}
		})
	}
}

// TestTheTwoSpellingsOfOneOperationSetAreOneLocator is AC #2's equivalence read as a locator
// rather than as a decoded value: a diagnostic about `operations` must name the same node
// whichever way its author wrote the set. It is the in-place half of the claim above, stated
// separately because it is the half AC #2 promises.
func TestTheTwoSpellingsOfOneOperationSetAreOneLocator(t *testing.T) {
	handWritten := valueOfEntry(normalized(t, "operations:\n  insert: {}\n"), 0).GetPath()
	folded := valueOfEntry(normalized(t, "operations: [insert]\n"), 0).GetPath()

	if handWritten == "" {
		t.Fatal("the hand-written mapping carries no locator either, so this would assert nothing")
	}
	if folded != handWritten {
		t.Errorf("the folded mapping is at %q and the hand-written one at %q; one document, two locators",
			folded, handWritten)
	}
}

// TestEveryFoldedOperationEntryCarriesALocator covers the third node the fold builds. The entry
// wrapping a folded operation is `ast.MappingValue`'s, which sets no path either, so it needs the
// locator of the two nodes it holds rather than the empty string they would otherwise disagree
// with.
func TestEveryFoldedOperationEntryCarriesALocator(t *testing.T) {
	_, root, _ := stageE(t, "operations: [insert, update]\n", nil)

	folded := valueOfEntry(root, 0).(*ast.MappingNode).Values
	if len(folded) != 2 {
		t.Fatalf("the fold produced %d entries, want 2; this would assert nothing", len(folded))
	}
	for _, entry := range folded {
		if got := entry.GetPath(); got != entry.Key.GetPath() {
			t.Errorf("the entry holding %q is at %q, want its key's own %q",
				entry.Key.String(), got, entry.Key.GetPath())
		}
	}
}

// lastValueOf is the value of a mapping's last entry, which is where a document that had to
// declare an anchor first writes the entry a test is about.
func lastValueOf(t *testing.T, root ast.Node) ast.Node {
	t.Helper()

	mapping, isMapping := root.(*ast.MappingNode)
	if !isMapping || len(mapping.Values) == 0 {
		t.Fatalf("the document is %T with no entries to read", root)
	}
	return mapping.Values[len(mapping.Values)-1].Value
}
