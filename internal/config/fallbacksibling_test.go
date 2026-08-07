package config

import (
	"strings"
	"testing"
)

// This file answers one decision the containment layouts lean on: which lines beneath a sensitive
// key the fallback keeps. It is separate from the layouts because the decision is the redactor's and
// the layouts merely depend on it -- a change of mind about it has to fail by name here, rather than
// as a grid row nobody can classify.

// TestTheFallbackKeepsAReadableSiblingOfAnUnreadableKey pins the decision the containment layouts
// lean on when they indent a value under its key, and it is the finding recorded by the corpus entry
// 46cb793b1852f94e: a secret holding a line break planted its remainder at column one, where those
// bytes read as a readable key of their own and the fallback kept them.
//
// Keeping such a line is deliberate. It sits at the sensitive key's *own* column, so YAML makes it a
// sibling entry rather than that key's value, and its name is exactly what is written and matches no
// sensitive one -- while blanking every readable key below a sensitive one would take from each
// diagnostic the context it exists to show. Both sides are asserted here, so a change of mind about
// either fails by name rather than as a containment row nobody can classify.
func TestTheFallbackKeepsAReadableSiblingOfAnUnreadableKey(t *testing.T) {
	const (
		hidden  = "PGNOTY-SIBLING-HIDDEN"
		sibling = "PGNOTY-SIBLING-KEPT"
	)
	// The indented line comes first, while the sensitive key's block is still open: the sibling below
	// it closes that block, which is the very decision under test.
	document := "secrets: " + hidden + "\n" +
		"  " + hidden + "-INDENTED\n" +
		sibling + ": " + sibling + "-VALUE\n" +
		unparseableTail

	if pathsAreResolvable(parseDocument(newSource("", []byte(document)))) {
		t.Fatalf("the document parses, so it cannot show what the fallback does:\n%s", document)
	}

	rendered := renderEveryLineOf(document)
	if strings.Contains(rendered, hidden) {
		t.Errorf("the fallback quoted the sensitive key's own value or a line indented under it:\n%s",
			rendered)
	}
	if !strings.Contains(rendered, sibling) {
		t.Errorf("the fallback blanked a readable sibling key at the sensitive key's own column; "+
			"every diagnostic below one would lose its context:\n%s", rendered)
	}
}
