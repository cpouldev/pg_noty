package config

import (
	"fmt"
	"testing"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// The eight places a reference can be written in a configuration, and the two AC #4 claims
// quantified over all of them.
//
// The layouts are their own declaration because both the table test and the fuzz target are
// quantified over the same dimension, and because a later step that adds a position a value
// can occupy adds it in one place (Implementation Note 12).

// referenceLayouts enumerates the positions a reference can occupy in a configuration, so
// the properties quantified over "any value, anywhere" generate the dimension they are
// quantified over rather than one hand-written document.
//
// It is closed over the arms of the code it quantifies over: every shape that carries a value --
// scalarTextOf's two, and valuePositionsOf's mapping, sequence, anchor and tag -- has a row here,
// which TestEveryValueBearingShapeHasALayout asserts by walking the layouts rather than by
// counting them.
//
// holds is not always the value itself: a block scalar's content carries the newline the
// block adds, so the expectation is derived per layout rather than assumed to be equal.
var referenceLayouts = []struct {
	name     string
	document func(reference string) string
	path     string
	holds    func(value string) string
}{
	{
		name:     "a plain scalar value",
		document: func(ref string) string { return "value: " + ref + "\n" },
		path:     "$.value",
		holds:    func(value string) string { return value },
	},
	{
		name:     "a single-quoted value",
		document: func(ref string) string { return "value: '" + ref + "'\n" },
		path:     "$.value",
		holds:    func(value string) string { return value },
	},
	{
		name:     "a double-quoted value",
		document: func(ref string) string { return "value: \"" + ref + "\"\n" },
		path:     "$.value",
		holds:    func(value string) string { return value },
	},
	{
		name:     "a block sequence element",
		document: func(ref string) string { return "values:\n  - " + ref + "\n" },
		path:     "$.values[0]",
		holds:    func(value string) string { return value },
	},
	{
		name:     "a flow sequence element",
		document: func(ref string) string { return "values: [\"" + ref + "\"]\n" },
		path:     "$.values[0]",
		holds:    func(value string) string { return value },
	},
	{
		name:     "a value nested two mappings deep",
		document: func(ref string) string { return "outer:\n  inner: " + ref + "\n" },
		path:     "$.outer.inner",
		holds:    func(value string) string { return value },
	},
	{
		name:     "a tagged value",
		document: func(ref string) string { return "value: !!str " + ref + "\n" },
		path:     "$.value",
		holds:    func(value string) string { return value },
	},
	{
		// The arm this list was missing. valuePositionsOf has a case for an anchored value --
		// yield the value, never the name -- and no layout generated one, so the property that
		// reads as "any value, anywhere" excluded the shape V2 names as the corruption risk.
		name:     "an anchored value",
		document: func(ref string) string { return "value: &shared " + ref + "\n" },
		path:     "$.value",
		holds:    func(value string) string { return value },
	},
	{
		// A block scalar is the shape V2 names as needing its own descent: its content
		// lives in an inner string node rather than in the block node itself.
		name:     "a block scalar",
		document: func(ref string) string { return "value: |\n  " + ref + "\n" },
		path:     "$.value",
		holds:    func(value string) string { return value + "\n" },
	},
}

// TestEveryValueBearingShapeHasALayout keeps the generated dimension closed over the arms of the
// code it is quantified over. A shape that can hold a value and that no layout writes turns a
// property reading "any value, anywhere" into one that silently excludes it -- which is what the
// anchored value was until this step, even though valuePositionsOf has an arm written for it.
//
// The shapes are collected by parsing each layout rather than named here, so a new layout joins
// the claim by existing and a new arm fails it by having nothing that produces its type.
func TestEveryValueBearingShapeHasALayout(t *testing.T) {
	written := make(map[string]bool)
	for _, layout := range referenceLayouts {
		descendValuePositions(parsedRoot(t, layout.document("${VALUE}")), func(node ast.Node) {
			written[fmt.Sprintf("%T", node)] = true
		})
	}

	valueBearing := []string{
		"*ast.StringNode", "*ast.LiteralNode",
		"*ast.MappingNode", "*ast.SequenceNode", "*ast.AnchorNode", "*ast.TagNode",
	}
	for _, shape := range valueBearing {
		if !written[shape] {
			t.Errorf("no layout writes a %s, so every property quantified over these layouts excludes it", shape)
		}
	}
}

// TestASubstitutedValueIsOpaqueInEveryPositionAValueCanOccupy is AC #4 over every layout: a
// substituted value is the environment's bytes exactly, the document keeps its line count,
// and every reported position is where it was before.
func TestASubstitutedValueIsOpaqueInEveryPositionAValueCanOccupy(t *testing.T) {
	for _, layout := range referenceLayouts {
		t.Run(layout.name, func(t *testing.T) {
			assertOpaqueSubstitution(t, layout.document("${VALUE}"), layout.path,
				MapEnv(map[string]string{"VALUE": adversarialValue}), layout.holds(adversarialValue))
		})
	}
}

// FuzzASubstitutedValueIsOpaqueWhereverItIsWritten is the property behind AC #4, quantified
// over the two dimensions the invariant actually covers: the bytes an environment variable
// can hold, and the position in a document a reference can be written in.
//
// It discriminates rather than restating the code. Interpolation over raw text -- the design
// this stage exists to reject -- fails it on any value holding a newline (the document gains
// lines and every position after the substitution moves), on any value holding a colon or a
// quote (the value is re-read as YAML), and on any value holding `${` (the substitution is
// scanned again).
//
// The oracle compares the whole value rather than a prefix, so a substitution that truncated
// or re-wrapped part of a multi-line value is visible.
func FuzzASubstitutedValueIsOpaqueWhereverItIsWritten(f *testing.F) {
	seeds := []string{
		adversarialValue,
		"",
		"plain",
		"${STILL_A_REFERENCE}",
		"$${ESCAPED}",
		"line one\nline two\nline three",
		"- not: a list\n# not a comment\n--- not a document",
		"\ttabbed\r\ncarriage returned",
	}
	for _, value := range seeds {
		for layout := range referenceLayouts {
			f.Add(value, uint8(layout))
		}
	}

	f.Fuzz(func(t *testing.T, value string, layout uint8) {
		chosen := referenceLayouts[int(layout)%len(referenceLayouts)]
		document := chosen.document("${VALUE}")

		if _, err := parser.ParseBytes([]byte(document), parser.ParseComments); err != nil {
			t.Fatalf("%s does not parse: %v", chosen.name, err)
		}
		assertOpaqueSubstitution(t, document, chosen.path,
			MapEnv(map[string]string{"VALUE": value}), chosen.holds(value))
	})
}
