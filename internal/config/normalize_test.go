package config

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// Stage E's walk: what an alias stands for, what an anchor leaves behind, and what the stage
// does with a shape it cannot descend. Merge keys are mergekey_test.go's subject and the
// operations sugar is listsugar_test.go's; the fixture corpus is anchorfixture_test.go's.

// stageE runs stages D and E over text and reports the normalized document together with the
// diagnostics stage E raised.
//
// Stage D runs first because the pipeline runs it first, and because that ordering is the whole
// reason an anchored `$${` is unescaped once rather than once per alias. A fixture that does not
// survive stage D fails here rather than quietly asserting nothing about stage E.
func stageE(t *testing.T, text string, vars map[string]string) (*source, ast.Node, Errors) {
	t.Helper()

	src, root, _, faults := stageD(t, text, vars)
	if len(faults) != 0 {
		t.Fatalf("the fixture does not reach stage E: %q\nsource:\n%s", messagesOf(faults), text)
	}
	return src, root, normalize(src, root)
}

// normalized runs stage E over a document whose references all resolve and fails the test when
// the stage reported anything, for the many cases whose subject is the tree rather than a
// diagnostic.
func normalized(t *testing.T, text string) ast.Node {
	t.Helper()

	_, root, diags := stageE(t, text, nil)
	if len(diags) != 0 {
		t.Fatalf("normalizing reported %q\nsource:\n%s", messagesOf(diags), text)
	}
	return root
}

// decodedDocument is what a consumer receives from a normalized tree: the library's own decoding
// of it. Every shape claim here reads this rather than the package's own node access, so a tree
// that only looks right to the code that built it cannot pass.
func decodedDocument(t *testing.T, root ast.Node) map[string]any {
	t.Helper()

	var decoded map[string]any
	if err := yaml.NodeToValue(root, &decoded); err != nil {
		t.Fatalf("decoding the normalized document failed: %v", err)
	}
	return decoded
}

// firstEntryOf is the first entry of a node that must be a mapping.
func firstEntryOf(t *testing.T, node ast.Node) *ast.MappingValueNode {
	t.Helper()

	mapping, isMapping := node.(*ast.MappingNode)
	if !isMapping {
		t.Fatalf("node is %T, want a mapping", node)
	}
	if len(mapping.Values) == 0 {
		t.Fatal("the mapping has no entries")
	}
	return mapping.Values[0]
}

// TestAnAliasResolvesToTheAnchoredValuesFullContent is SC-1's unit half: an alias is not a
// reference to be followed later, it is replaced by what the anchor holds, in full.
func TestAnAliasResolvesToTheAnchoredValuesFullContent(t *testing.T) {
	const document = "base: &shared\n  url: https://example.test\n  method: POST\nuse: *shared\n"

	decoded := decodedDocument(t, normalized(t, document))

	used, isMapping := decoded["use"].(map[string]any)
	if !isMapping {
		t.Fatalf("use decoded to %#v, want the anchored mapping", decoded["use"])
	}
	if used["url"] != "https://example.test" || used["method"] != "POST" {
		t.Errorf("use = %#v, want every key the anchor holds", used)
	}
}

// TestAnAnchorIsReplacedByTheValueItNames records the reduction an anchor undergoes: after this
// stage the document holds the value, not the wrapper naming it, so nothing downstream needs an
// anchor case. The anchor name stays available to aliases through the table, not through the tree.
func TestAnAnchorIsReplacedByTheValueItNames(t *testing.T) {
	root := normalized(t, "base: &shared\n  url: https://example.test\nuse: *shared\n")

	defined := valueOfEntry(root, 0)
	if _, stillWrapped := defined.(*ast.AnchorNode); stillWrapped {
		t.Error("the anchor wrapper survived normalization, so a later stage still has to unwrap it")
	}
	if defined != valueOfEntry(root, 1) {
		t.Error("the alias resolved to a different node from the anchor's own value; the two must be one node")
	}
}

// TestAnAliasIsResolvedInEveryPositionAValueCanBeWritten closes the walk over the shapes it
// descends: a value reached through a mapping, a sequence or a tag is normalized like any other,
// so an alias cannot survive in a position nothing rewrites.
func TestAnAliasIsResolvedInEveryPositionAValueCanBeWritten(t *testing.T) {
	const anchor = "base: &shared text\n"

	positions := map[string]struct {
		document string
		path     string
	}{
		"a mapping value":           {document: anchor + "use: *shared\n", path: "$.use"},
		"a value two mappings deep": {document: anchor + "outer:\n  inner: *shared\n", path: "$.outer.inner"},
		"a block sequence element":  {document: anchor + "list:\n  - *shared\n", path: "$.list[0]"},
		"a flow sequence element":   {document: anchor + "list: [*shared]\n", path: "$.list[0]"},
		"a tagged value":            {document: anchor + "use: !!str *shared\n", path: "$.use"},
		"a value inside an anchor":  {document: anchor + "outer: &other\n  inner: *shared\n", path: "$.outer.inner"},
	}

	for name, position := range positions {
		t.Run(name, func(t *testing.T) {
			root := normalized(t, position.document)

			if got := textIn(t, root, position.path); got != "text" {
				t.Errorf("%s decoded to %q, want the anchored text; an alias survived here", position.path, got)
			}
		})
	}
}

// TestASequenceElementStaysOneNodeInBothFieldsThatShareItAfterResolution holds the invariant
// TestGoccyGivesASequenceTheSameNodesInValuesAndEntries pins for the parser: Values[i] is the
// node Entries[i].Value is. A replacement written through one field only leaves a tree where the
// two disagree, and the next reader of Entries would find the alias this stage removed.
func TestASequenceElementStaysOneNodeInBothFieldsThatShareItAfterResolution(t *testing.T) {
	root := normalized(t, "base: &shared text\nlist:\n  - *shared\n  - plain\n")
	sequence := valueOfEntry(root, 1).(*ast.SequenceNode)

	if len(sequence.Values) != len(sequence.Entries) {
		t.Fatalf("Values holds %d nodes and Entries %d", len(sequence.Values), len(sequence.Entries))
	}
	for i := range sequence.Values {
		if sequence.Values[i] != sequence.Entries[i].Value {
			t.Errorf("Values[%d] is not Entries[%d].Value; the resolved alias is visible through one field only", i, i)
		}
	}
}

// TestAnUndefinedAliasYieldsExactlyOnePositionedDiagnostic is the first of stage E's two error
// conditions. The parser reports none of it (V7), so the whole diagnostic is this stage's.
func TestAnUndefinedAliasYieldsExactlyOnePositionedDiagnostic(t *testing.T) {
	// `use: ` is five runes, so the alias begins at rune 6 of line 1.
	_, _, diags := stageE(t, "use: *nowhere\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want exactly one", len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], undefinedAlias, 1, 6)
	if diags[0].Path != "use" {
		t.Errorf("Path = %q, want the alias's own locator", diags[0].Path)
	}
}

// assertDiagnostic checks the four things every stage-E diagnostic must carry: which stage
// raised it, what it says, what it suggests, and where it points.
func assertDiagnostic(t *testing.T, diag Error, why fault, line, column int) {
	t.Helper()

	if diag.Rule != RuleNormalize {
		t.Errorf("Rule = %q, want %q", diag.Rule, RuleNormalize)
	}
	if diag.Msg != why.message {
		t.Errorf("Msg = %q, want %q", diag.Msg, why.message)
	}
	if diag.Hint != why.hint {
		t.Errorf("Hint = %q, want %q", diag.Hint, why.hint)
	}
	if diag.Line != line || diag.Col != column {
		t.Errorf("position is %d:%d, want %d:%d", diag.Line, diag.Col, line, column)
	}
}
