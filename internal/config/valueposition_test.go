package config

import (
	"slices"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// The value-position rule: where a reference may be applied at all (valueposition.go).
//
// V2's rule is asserted against the shapes themselves rather than through a substitution,
// which is what lets a mapping key, an anchor name and an alias name -- all *ast.StringNode
// -- be told apart by the edge each was reached through. No substitution-level test could
// falsify that as sharply.
//
// The enumerations that prove nothing unrecognised arrives are documentshape_test.go's, and
// the refusals they make affordable are unreadableshape_test.go's; neither half means anything
// without the other.

// valuePositionCases is V2 made structural, one row per shape a document can hold: what the
// stage reaches through a value edge, what it reaches through a key edge, and whether it
// recognises the shape at all.
//
// A type switch on *ast.StringNode passes none of these rows, because mapping keys, anchor
// names and alias names all carry that type.
var valuePositionCases = []struct {
	name       string
	document   string
	node       func(root ast.Node) ast.Node
	wantValues []string
	wantKeys   []string
	wantKnown  bool
}{
	{
		name:       "a mapping yields its values and its keys, never one as the other",
		document:   "first: ${A}\nsecond: ${B}\n",
		node:       func(root ast.Node) ast.Node { return root },
		wantValues: []string{"${A}", "${B}"},
		wantKeys:   []string{"first", "second"},
		wantKnown:  true,
	},
	{
		name:       "a sequence yields its elements",
		document:   "l:\n  - ${A}\n  - ${B}\n",
		node:       func(root ast.Node) ast.Node { return valueOfFirstEntry(root) },
		wantValues: []string{"${A}", "${B}"},
		wantKnown:  true,
	},
	{
		name:       "an anchor yields its value and never its name",
		document:   "a: &NAME ${A}\n",
		node:       func(root ast.Node) ast.Node { return valueOfFirstEntry(root) },
		wantValues: []string{"${A}"},
		wantKnown:  true,
	},
	{
		name:      "an alias yields nothing, because it holds a name rather than content",
		document:  "a: &NAME ${A}\nb: *NAME\n",
		node:      func(root ast.Node) ast.Node { return valueOfEntry(root, 1) },
		wantKnown: true,
	},
	{
		name:       "a tag yields the value it tags",
		document:   "a: !!str ${A}\n",
		node:       func(root ast.Node) ast.Node { return valueOfFirstEntry(root) },
		wantValues: []string{"${A}"},
		wantKnown:  true,
	},
	{
		name:      "a scalar the parser already read as a number yields nothing",
		document:  "a: 16\n",
		node:      func(root ast.Node) ast.Node { return valueOfFirstEntry(root) },
		wantKnown: true,
	},
	{
		name:      "a sequence entry wrapper is not a shape this stage recognises",
		document:  "l:\n  - ${A}\n",
		node:      func(root ast.Node) ast.Node { return firstSequenceEntry(root) },
		wantKnown: false,
	},
	{
		// The mapping entry is the sequence wrapper's twin, and is excluded for the same
		// reason: a mapping yields its entries' keys and values directly, so an entry is
		// never reached and answering for one would be a second path to the same nodes
		// (Implementation Note 4 -- a nested mapping arrives as an *ast.MappingNode, so no
		// entry is ever handed to this function). Its key is not offered either, which is
		// what keeps that second path from existing on the key side.
		name:      "a mapping entry is not a shape this stage recognises either",
		document:  "first: ${A}\n",
		node:      firstMappingEntry,
		wantKnown: false,
	},
}

// TestValuePositionsExcludeKeysAnchorNamesAndAliasNames walks valuePositionCases: the
// recursion decides what a value is by the parent relationship, and this asserts that
// decision directly for every shape.
func TestValuePositionsExcludeKeysAnchorNamesAndAliasNames(t *testing.T) {
	for _, tc := range valuePositionCases {
		t.Run(tc.name, func(t *testing.T) {
			root := parsedRoot(t, tc.document)
			node := tc.node(root)

			values, known := valuePositionsOf(node)
			if known != tc.wantKnown {
				t.Errorf("valuePositionsOf(%T) recognised = %v, want %v", node, known, tc.wantKnown)
			}
			if got := renderedTexts(values); !slices.Equal(got, tc.wantValues) {
				t.Errorf("values = %q, want %q", got, tc.wantValues)
			}
			if got := renderedTexts(keyPositionsOf(node)); !slices.Equal(got, tc.wantKeys) {
				t.Errorf("keys = %q, want %q", got, tc.wantKeys)
			}
		})
	}
}

func renderedTexts(nodes []ast.Node) []string {
	if len(nodes) == 0 {
		return nil
	}
	texts := make([]string, len(nodes))
	for i, node := range nodes {
		texts[i] = node.String()
	}
	return texts
}

func valueOfEntry(root ast.Node, index int) ast.Node {
	return root.(*ast.MappingNode).Values[index].Value
}

func firstMappingEntry(root ast.Node) ast.Node {
	return root.(*ast.MappingNode).Values[0]
}

func valueOfFirstEntry(root ast.Node) ast.Node {
	return valueOfEntry(root, 0)
}

func firstSequenceEntry(root ast.Node) ast.Node {
	return valueOfFirstEntry(root).(*ast.SequenceNode).Entries[0]
}

// TestGoccyGivesABlockScalarItsContentInAnInnerStringNode pins the field access scalarTextOf
// makes on every block scalar.
//
// `*ast.LiteralNode.Value` is dereferenced both to read a block's content and to rewrite it,
// so a library version that left it nil would surface as a panic inside the loader rather than
// as a named failing test. The two empty rows are where a missing inner node would appear
// first: a block with no content still has to have somewhere for that content to be.
func TestGoccyGivesABlockScalarItsContentInAnInnerStringNode(t *testing.T) {
	blocks := map[string]struct{ document, want string }{
		"a block literal":        {document: "value: |\n  text\n", want: "text\n"},
		"a folded block":         {document: "value: >\n  text\n", want: "text\n"},
		"an empty block literal": {document: "value: |\n", want: ""},
		"an empty folded block":  {document: "value: >\n", want: ""},
	}

	for name, block := range blocks {
		t.Run(name, func(t *testing.T) {
			node := valueOfFirstEntry(parsedRoot(t, block.document))

			literal, isLiteral := node.(*ast.LiteralNode)
			if !isLiteral {
				t.Fatalf("the block is %T, want *ast.LiteralNode", node)
			}
			if literal.Value == nil {
				t.Fatal("LiteralNode.Value is nil, so scalarTextOf would panic on this shape")
			}
			if got := literal.Value.Value; got != block.want {
				t.Errorf("the block's content is %q, want %q", got, block.want)
			}
		})
	}
}
