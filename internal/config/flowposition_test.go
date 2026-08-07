package config

import (
	"strings"
	"testing"
)

// The one arrangement in which the *column* half of the position comparison decides anything: two
// keys of one mapping share a line only in flow style, and a hand-written flow mapping is already
// in slice order -- so the column matters only once a merge has reordered a flow mapping whose
// anchor sits on the same line as it.

// twoHeadersOnOneLine is the last arrangement the comparator needs, and the only one in which its
// *column* half decides anything.
//
// Two keys of one mapping share a line only in flow style, and a hand-written flow mapping is in
// slice order anyway -- so the column matters only when a merge has reordered a flow mapping whose
// anchor sits on the same line. The whole document is therefore one line: the anchored `X-T` is
// written before the merging mapping and so at the smaller column, while the expansion appends it
// *after* the directly written `x-t`. Line order alone ties, and a stable sort then answers slice
// order, which is the wrong one.
const twoHeadersOnOneLine = `{version: 1, database: {url: postgres://noty:pw@db/noty}, ` +
	`defaults: {headers: &s {X-T: a}}, ` +
	`listeners: [{name: n, table: s.t, operations: [insert], ` +
	`destination: {url: https://h.test/x, headers: {<<: *s, x-t: b}}}]}
`

// TestTheColumnDecidesTheFirstOccurrenceWhenTwoNamesShareALine pins that half.
//
// Both expected columns are computed from the document rather than written down: the line is ASCII
// throughout, so the rune column of a name is one past the offset of its only occurrence.
func TestTheColumnDecidesTheFirstOccurrenceWhenTwoNamesShareALine(t *testing.T) {
	anchored := strings.Index(twoHeadersOnOneLine, "X-T:") + 1
	direct := strings.Index(twoHeadersOnOneLine, "x-t:") + 1
	if anchored >= direct {
		t.Fatalf("the anchored name is at column %d and the direct one at %d; this case needs the "+
			"anchored one earlier on the line", anchored, direct)
	}

	src, root, faults := stageE(t, twoHeadersOnOneLine, corpusVariables)
	if len(faults) != 0 {
		t.Fatalf("the fixture does not reach stage F: %q", messagesOf(faults))
	}

	// Without this the case would pass on a mapping the expansion happened to leave in column
	// order, which is the arrangement it exists to rule out.
	headers := nodeIn(t, root, "$.listeners[0].destination.headers")
	if names := namesWrittenIn(t, headers); !equalStrings(names, []string{"x-t", "X-T"}) {
		t.Fatalf("the expanded mapping holds %v; this case needs the anchored name last", names)
	}

	diags, _ := checkShape(src, root)

	if len(diags) != 1 {
		t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
	}
	if diags[0].Col != direct {
		t.Errorf("the repeat is reported at column %d, want %d -- the name written later on the line",
			diags[0].Col, direct)
	}
}

// TestNamesDifferingOnlyInCaseAreOneNameOnlyInAHeaderMapping is the other side of R20's reach, and
// it is the side the schema flag alone cannot assert: `httpFieldNames` being declared on one level
// says nothing about which levels the walk actually applies the rule at.
//
// Case-insensitivity is true of an HTTP field name and of nothing else the contract declares.
// `Version` beside `version` at the document root is one unknown key, not a header written twice --
// and a walk that folded case everywhere would report it as both, calling a root key a header.
func TestNamesDifferingOnlyInCaseAreOneNameOnlyInAHeaderMapping(t *testing.T) {
	document := "version: 1\nVersion: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\nlisteners: []\n"

	diags, _ := stageF(t, document)

	if len(diags) != 1 {
		t.Fatalf("%d diagnostics, want exactly 1 -- the unknown key: %q", len(diags), messagesOf(diags))
	}
	if diags[0].Rule != R41 {
		t.Errorf("Rule = %q, want R41; two root keys differing in case are not two headers", diags[0].Rule)
	}
	for _, diag := range diags {
		if strings.Contains(diag.Msg, "header") {
			t.Errorf("a root key was reported as a header: %q", diag.Msg)
		}
	}
}
