package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// Which of two keys naming one thing an author wrote *first*, which every first-occurrence rule in
// the package depends on and which Step 4's Implementation Note 6 made a decision rather than an
// accident: after a merge expansion a mapping's entries are no longer in document order, because an
// inherited entry carries the anchor's own line while sitting last in the slice.
//
// Three arrangements, because the comparison has two halves and the obvious arrangement exercises
// neither cleanly: an anchor shallower than the merge site (slice order is wrong, line order is
// right), an anchor deeper than it (column order is also wrong), and both names on one line (only
// the column can decide).

// headersMergedFromAnAnchor puts the two colliding names in the one order that tells the two
// implementations apart. The inherited `X-Trace` carries the anchor's own line 6 while sitting
// *last* in the expanded mapping, so a rule reading slice order would call the earlier name the
// repeat and send its author to line 15 to fix a header written on line 6.
//
// Lines: 1 version, 2 database, 3 url, 4 defaults, 5 headers, 6 `    X-Trace: a`, 7 listeners,
// 8 name, 9 table, 10 operations, 11 destination, 12 url, 13 headers, 14 `<<`,
// 15 `        x-trace: b`.
const headersMergedFromAnAnchor = `version: 1
database:
  url: postgres://noty:pw@db.internal:5432/noty
defaults:
  headers: &shared
    X-Trace: a
listeners:
  - name: order_paid
    table: public.orders
    operations: [insert]
    destination:
      url: https://hooks.example.test/order-paid
      headers:
        <<: *shared
        x-trace: b
`

// TestTheFirstOccurrenceIsDecidedByPositionRatherThanByEntryOrder is Implementation Note 6's
// consequence, asserted rather than commented. After a merge expansion a mapping's entries are no
// longer in document order, and every first-occurrence rule in the package inherits that.
func TestTheFirstOccurrenceIsDecidedByPositionRatherThanByEntryOrder(t *testing.T) {
	src, root, faults := stageE(t, headersMergedFromAnAnchor, corpusVariables)
	if len(faults) != 0 {
		t.Fatalf("the fixture does not reach stage F: %q", messagesOf(faults))
	}

	// Without this the assertion below could pass on a document whose entries happened to be in
	// document order after all, which is the arrangement it exists to rule out.
	names := namesWrittenIn(t, nodeIn(t, root, "$.listeners[0].destination.headers"))
	if !equalStrings(names, []string{"x-trace", "X-Trace"}) {
		t.Fatalf("the expanded mapping holds %v; this case needs the inherited name last", names)
	}

	diags, _ := checkShape(src, root)

	if len(diags) != 1 {
		t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
	}
	if diags[0].Line != 15 {
		t.Errorf("the repeat is reported on line %d, want 15 -- the name written later in the file",
			diags[0].Line)
	}
	if !strings.Contains(diags[0].Msg, "line 6") {
		t.Errorf("Msg = %q, want it to name line 6, where the inherited header is written", diags[0].Msg)
	}
}

// namesWrittenIn is the key text of every entry of a mapping, in the order the slice holds them.
func namesWrittenIn(t *testing.T, node ast.Node) []string {
	t.Helper()

	mapping, isMapping := node.(*ast.MappingNode)
	if !isMapping {
		t.Fatalf("node is %T, want a mapping", node)
	}

	var names []string
	for _, entry := range mapping.Values {
		text, _ := keyTextOf(entry.Key)
		names = append(names, text)
	}
	return names
}

// headersMergedFromADeeperAnchor is the other arrangement of the same merge, and it is the one that
// makes the *line* half of the position comparison load-bearing rather than the column half.
//
// Every key of a hand-written mapping shares one indentation, so within a single mapping a
// comparison on the column alone collapses to slice order and happens to answer correctly. An
// inherited entry is the exception: it comes from a mapping indented differently. In
// headersMergedFromAnAnchor the anchor is the *shallower* of the two, so column order and line
// order agree; here it is the deeper, so they disagree, and only the line answers correctly.
//
// Lines: 1 version, 2 database, 3 url, 4 listeners, 5 name, 6 table, 7 operations, 8 destination,
// 9 its url, 10 headers, 11 `        X-Trace: a` (rune 9), 12 defaults, 13 headers, 14 `<<`,
// 15 `    x-trace: b` (rune 5).
const headersMergedFromADeeperAnchor = `version: 1
database:
  url: postgres://noty:pw@db.internal:5432/noty
listeners:
  - name: order_paid
    table: public.orders
    operations: [insert]
    destination:
      url: https://hooks.example.test/order-paid
      headers: &shared
        X-Trace: a
defaults:
  headers:
    <<: *shared
    x-trace: b
`

// TestTheFirstOccurrenceIsDecidedByTheLineAndNotOnlyTheColumn pins the half above that the other
// arrangement cannot reach.
func TestTheFirstOccurrenceIsDecidedByTheLineAndNotOnlyTheColumn(t *testing.T) {
	src, root, faults := stageE(t, headersMergedFromADeeperAnchor, corpusVariables)
	if len(faults) != 0 {
		t.Fatalf("the fixture does not reach stage F: %q", messagesOf(faults))
	}

	// The inherited name must sit at the *greater* column, or the arrangement is the other one and
	// this case asserts nothing new.
	inherited := positionOf(src, entryKeyNamed(t, nodeIn(t, root, "$.defaults.headers"), "X-Trace"))
	direct := positionOf(src, entryKeyNamed(t, nodeIn(t, root, "$.defaults.headers"), "x-trace"))
	if inherited.Col() <= direct.Col() {
		t.Fatalf("the inherited name is at column %d and the direct one at %d; this case needs the "+
			"inherited one deeper", inherited.Col(), direct.Col())
	}

	diags, _ := checkShape(src, root)

	if len(diags) != 1 {
		t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
	}
	if diags[0].Line != 15 {
		t.Errorf("the repeat is reported on line %d, want 15 -- the name written later in the file",
			diags[0].Line)
	}
	if !strings.Contains(diags[0].Msg, "line 11") {
		t.Errorf("Msg = %q, want it to name line 11, where the inherited header is written", diags[0].Msg)
	}
}

// entryKeyNamed is the key node of the entry a mapping writes under name.
func entryKeyNamed(t *testing.T, node ast.Node, name string) ast.Node {
	t.Helper()

	mapping, isMapping := node.(*ast.MappingNode)
	if !isMapping {
		t.Fatalf("node is %T, want a mapping", node)
	}
	for _, entry := range mapping.Values {
		if text, readable := keyTextOf(entry.Key); readable && text == name {
			return entry.Key
		}
	}

	t.Fatalf("the mapping holds no entry called %q", name)
	return nil
}
