package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// The keystone's own gate: every wrapper returns nil however the value is written, and records what
// it could not read as one positioned diagnostic instead. Presence -- absent against explicitly
// falsy -- is presence_test.go's subject, and the stage as a whole is decode_test.go's.
//
// ADR-2 rests on the return value alone. A single wrapper that can answer non-nil collapses the
// accumulate model to first-error-only, because one NodeToValue call then abandons every field after
// the first mistake -- so the evidence here is per wrapper and adversarial, never one shared case
// (the step's Post-Breakdown Review makes this the gate the step is returned on).

// reading is what one wrapper recorded from one node: the three states it distinguishes, the value
// it read rendered as text, and the diagnostics stage G resolves out of it.
//
// The value is rendered rather than typed so that one table can assert every wrapper's answer. It is
// the typed value's own rendering -- 10s for a time.Duration, 16 for an int -- so a wrapper that
// kept the text instead of converting it still fails the rows that convert.
type reading struct {
	set     bool
	valid   bool
	value   string
	diags   Errors
	written Positioned
}

// wrapperKind is one of the five wrappers, together with the two things a case does to it: hand it a
// node, and read back what it recorded.
//
// A closure per wrapper rather than an interface, because Set is a field: what a case needs is not a
// behaviour the five share but the same four questions asked of five concrete types.
type wrapperKind struct {
	name string
	// read hands node to a fresh wrapper of this kind and reports what it recorded, failing the
	// test if UnmarshalYAML answers non-nil -- which is the guarantee every case here rests on.
	//
	// The source is taken alongside the node because a column is derived against the raw line the
	// token sits on (ADR-4): a case that resolved against any other text would report column 1 for
	// every value and could not tell a right position from a wrong one.
	read func(t *testing.T, src *source, node ast.Node) reading
}

// wrapperKinds is the five wrappers scalar.go and scalarlist.go declare. The count is pinned by
// TestEveryWrapperIsCoveredHere, so a sixth wrapper cannot be added without a row.
var wrapperKinds = []wrapperKind{
	{name: "Str", read: func(t *testing.T, src *source, node ast.Node) reading {
		var held Str
		return readInto(t, src, &held, node, func() string { return held.value })
	}},
	{name: "Int", read: func(t *testing.T, src *source, node ast.Node) reading {
		var held Int
		return readInto(t, src, &held, node, func() string { return fmt.Sprint(held.value) })
	}},
	{name: "Bool", read: func(t *testing.T, src *source, node ast.Node) reading {
		var held Bool
		return readInto(t, src, &held, node, func() string { return fmt.Sprint(held.value) })
	}},
	{name: "Dur", read: func(t *testing.T, src *source, node ast.Node) reading {
		var held Dur
		return readInto(t, src, &held, node, func() string { return fmt.Sprint(held.value) })
	}},
	{name: "StrList", read: func(t *testing.T, src *source, node ast.Node) reading {
		var held StrList
		return readInto(t, src, &held, node, func() string { return strings.Join(textsIn(held), ",") })
	}},
}

// readInto is the one place a wrapper is handed a node, so the never-fail assertion cannot be
// forgotten by a row: every case in this file goes through it, and a non-nil answer fails the test
// that asked rather than being reported as a wrong value later.
//
// It resolves the wrapper against a source afterwards because that is what stage G does, and because
// resolving is what turns a recorded refusal into the diagnostic a caller sees -- so a wrapper that
// recorded nothing and a wrapper whose refusal never becomes a diagnostic are told apart here.
func readInto(t *testing.T, src *source, held decodedValue, node ast.Node, rendered func() string) reading {
	t.Helper()

	if err := held.UnmarshalYAML(node); err != nil {
		t.Fatalf("UnmarshalYAML returned %v; a wrapper that can fail collapses the whole accumulate model "+
			"to first-error-only (ADR-2)", err)
	}

	pass := newDecodePass(src)
	held.resolve(pass)

	return reading{
		set:     held.wasWritten(),
		valid:   held.Valid(),
		value:   rendered(),
		diags:   pass.diags,
		written: held,
	}
}

// decodedValue is what the four questions above are asked of. wasWritten() stands in for the Set field,
// which an interface cannot carry.
type decodedValue interface {
	Positioned
	resolvable
	UnmarshalYAML(ast.Node) error
	Valid() bool
	wasWritten() bool
}

// nodeReadBy is the three arguments a wrapper case is driven by: the test, the source a position is
// derived against, and the node at path within it. Every case goes through it, so no case can resolve a
// node against text the node did not come from -- which would report column 1 for every value and could
// not tell a right position from a wrong one.
func nodeReadBy(t *testing.T, document, path string) (*testing.T, *source, ast.Node) {
	t.Helper()

	src, node := nodeAt(t, document, path)
	return t, src, node
}

// textsIn is the text of every element of a list, which is what a case about StrList asserts.
func textsIn(held StrList) []string {
	texts := make([]string, 0, len(held.values))
	for _, element := range held.values {
		texts = append(texts, element.value)
	}
	return texts
}

// TestEveryWrapperReturnsNilForAnInvalidValueOfItsOwnType is the step's gate, and it is deliberately
// five adversarial cases rather than one: the guarantee is per implementation, and a shared case
// proves only that the wrapper it happened to run through returns nil.
//
// Each input is invalid *for the wrapper it is given to* -- text where a number is declared, a
// mapping where text is -- so a wrapper that answered nil only because its input was convertible
// would not be covered by its own row.
func TestEveryWrapperReturnsNilForAnInvalidValueOfItsOwnType(t *testing.T) {
	adversarial := map[string]string{
		// A mapping is the one shape no scalar can be read as, and it is what stage F would have
		// refused before decode: this row reaches the refusal directly.
		"Str": "value: {a: 1}\n",
		"Int": "value: abc\n",
		// Not "notabool": YAML reads 1 as an integer, and an integer is exactly the value an author
		// most plausibly writes for a flag, so the row that must fail is the one that looks closest
		// to succeeding.
		"Bool":    "value: 1\n",
		"Dur":     "value: 7d\n",
		"StrList": "value: notalist\n",
	}

	for _, kind := range wrapperKinds {
		t.Run(kind.name, func(t *testing.T) {
			document, covered := adversarial[kind.name]
			if !covered {
				t.Fatalf("no adversarial input for %s; the gate is per wrapper", kind.name)
			}

			// readInto fails the test on a non-nil answer, which is this test's first claim.
			got := kind.read(nodeReadBy(t, document, "$.value"))

			if len(got.diags) != 1 {
				t.Fatalf("recorded %d diagnostics %q, want exactly one: a conversion failure that "+
					"records none is silently zeroed, and one that records two double-reports",
					len(got.diags), messagesOf(got.diags))
			}
			if got.diags[0].Rule == noRule {
				t.Error("the recorded diagnostic names no rule, so Step 13 cannot inventory it")
			}
			if got.diags[0].Line != 1 {
				t.Errorf("recorded at line %d, want line 1 -- the line the value is written on", got.diags[0].Line)
			}
			if got.valid {
				t.Error("Valid() is true for a value the wrapper could not read")
			}
			if !got.set {
				t.Error("Set is false for a key the file wrote; only absence yields false")
			}
		})
	}
}

// scalarWrapperNames is the five wrappers as the step names them. It is read by the closure above
// and by the source scan below, so "these five exist" and "these five are covered" are one list.
var scalarWrapperNames = []string{"Str", "Int", "Bool", "Dur", "StrList"}
