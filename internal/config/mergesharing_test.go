package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// What listeners that merge one anchor share afterwards, which is the contract Implementation
// Note 6 hands to Steps 5-11: an inherited entry is not a copy, it is the anchor's own node.
//
// The value clauses next door (anchorfixture_test.go) all pass whether the entries are shared or
// copied, so the sharing needs an assertion of its own or it is prose.

// TestAnInheritedEntryIsOneNodeInEveryListenerThatMergedIt is that contract, asserted rather than
// written down. Steps 5-11 must treat a merged entry as read-only, and the reason is this: the
// listeners hold the anchor's own entry, which is exactly what gives their diagnostics one
// position and collapses them into one (ADR-3).
//
// A later stage that copied before writing would keep every value assertion green and silently
// lose the de-duplication. This is the assertion that fails instead.
func TestAnInheritedEntryIsOneNodeInEveryListenerThatMergedIt(t *testing.T) {
	root := anchorCorpusRoot(t)

	first := entryNamed(t, root, "$.listeners[0].retry", "backoff")
	if second := entryNamed(t, root, "$.listeners[1].retry", "backoff"); first != second {
		t.Error("the two listeners hold different nodes for one inherited entry; the merge copied " +
			"rather than shared, so a diagnostic about it now reaches the author twice")
	}
}

// entryNamed is the entry called name in the mapping at path.
func entryNamed(t *testing.T, root ast.Node, path, name string) *ast.MappingValueNode {
	t.Helper()

	mapping, isMapping := nodeIn(t, root, path).(*ast.MappingNode)
	if !isMapping {
		t.Fatalf("%s is %T, want a mapping", path, nodeIn(t, root, path))
	}
	for _, entry := range mapping.Values {
		if text, readable := keyTextOf(entry.Key); readable && text == name {
			return entry
		}
	}

	t.Fatalf("%s holds no entry called %q", path, name)
	return nil
}
