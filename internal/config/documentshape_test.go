package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// Every shape a configuration can actually write, enumerated: the corpus that makes stage D's two
// unconditional refusals affordable.
//
// The refusals themselves are asserted in unreadableshape_test.go. These enumerations say only
// that nothing arrives at one by accident, which is the half that decides whether refusing
// everything unlisted is safe.

// valueShapeDocuments is the value shapes this library version produces, each obtained by
// parsing the YAML that produces it rather than by naming a type. A library version that
// introduced a new one fails here, by name, before it can fail as a refused configuration.
//
// The merge-source spellings are a second value position and are enumerated separately, in
// mergesourceshape_test.go, which Step 4 owns. everyValueShapeDocument joins the two, and is what
// every property over "a shape a document can write in a value position" ranges over.
var valueShapeDocuments = map[string]string{
	"plain string":         "value: text\n",
	"single-quoted string": "value: 'text'\n",
	"double-quoted string": "value: \"text\"\n",
	"integer":              "value: 16\n",
	"float":                "value: 1.5\n",
	"boolean":              "value: true\n",
	"null":                 "value: ~\n",
	"omitted value":        "value:\n",
	"infinity":             "value: .inf\n",
	"not a number":         "value: .nan\n",
	"block mapping":        "value:\n  inner: text\n",
	"flow mapping":         "value: {inner: text}\n",
	"block sequence":       "value:\n  - one\n",
	"flow sequence":        "value: [one]\n",
	"empty sequence":       "value: []\n",
	"anchor":               "value: &shared text\n",
	"alias":                "shared: &shared text\nvalue: *shared\n",
	"tag":                  "value: !!str text\n",
	"binary tag":           "value: !!binary aGk=\n",
	"block literal":        "value: |\n  text\n",
	"folded block":         "value: >\n  text\n",
	"empty block literal":  "value: |\n",
	"empty folded block":   "value: >\n",

	// The package parses with parser.ParseComments, so a corpus that held no comment
	// would license the unconditional refusal over a document set excluding the one node
	// kind that parse mode exists to produce. Every placement a configuration can write
	// one in is here; each attaches to the node it annotates rather than becoming a value.
	"leading comment":                        "# leading\nvalue: text\n",
	"trailing comment on a value":            "value: text # trailing\n",
	"comment on the key line":                "value: # on the key line\n  inner: text\n",
	"comment above a nested key":             "value:\n  # interior\n  inner: text\n",
	"comment between two entries":            "value: text\n# between\nother: text\n",
	"comment inside a sequence":              "value:\n  # about the list\n  - one\n",
	"trailing comment on a sequence element": "value:\n  - one # trailing\n",
	"trailing comment on a flow sequence":    "value: [one, two] # trailing\n",
	"comment on a block scalar indicator":    "value: | # indicator\n  text\n",
	"a value that is only a comment":         "value:\n  # only a comment\n",
	"a trailing comment ending the document": "value: text\n# last word\n",
}

// keyShapeDocuments is the key shapes this library version produces, obtained the same way.
//
// `<<` is the row that carries the most weight: a merge key is a key shape, so a stage that
// refused what it had not listed would reject every document the merge feature exists for --
// and the merge corpus arrives in Step 4, after this guard was written.
//
// One shape a document can write in a key position is deliberately absent: an alias, which
// this stage refuses because stage E turns it into the substituted value the key check exists
// to prevent (TestAnAliasKeyIsRefusedBecauseStageEResolvesItToASubstitutedValue). Complex keys
// (`? [a, b]`, `? {a: b}`) do not parse in this library version, so they reach neither answer.
var keyShapeDocuments = map[string]string{
	"plain key":               "key: v\n",
	"single-quoted key":       "'key': v\n",
	"double-quoted key":       "\"key\": v\n",
	"explicit key":            "? key\n: v\n",
	"explicit block scalar":   "? |\n  text\n: v\n",
	"explicit folded block":   "? >\n  text\n: v\n",
	"anchored key":            "&k key: v\n",
	"explicitly anchored key": "? &k key\n: v\n",
	"tagged key":              "!!str key: v\n",
	"binary tagged key":       "!!binary aGk=: v\n",
	"merge key":               "a: &a {p: 1}\nb:\n  <<: *a\n",
	"integer key":             "16: v\n",
	"float key":               "1.5: v\n",
	"boolean key":             "true: v\n",
	"null key":                "~: v\n",
	"infinity key":            ".inf: v\n",
	"not-a-number key":        ".nan: v\n",
}

// TestEveryShapeADocumentCanHoldInAValuePositionIsRecognised is the reason the value path's
// unconditional refusal is safe: nothing a configuration can actually write reaches it.
func TestEveryShapeADocumentCanHoldInAValuePositionIsRecognised(t *testing.T) {
	for name, document := range everyValueShapeDocument() {
		t.Run(name, func(t *testing.T) {
			root := parsedRoot(t, document)
			for _, node := range everyValueNodeIn(root) {
				if scalar, holdsText := scalarTextOf(node); holdsText {
					// Recognising a scalar is only half the claim: the stage goes on to read
					// it, and a block scalar's text lives one level down in a node this
					// dereferences. The empty-block rows are where that inner node would be
					// missing, so the read is the assertion -- it panics rather than reports.
					scalar.written()
					continue
				}
				if _, recognised := valuePositionsOf(node); !recognised {
					t.Errorf("%T is written by a document that parses, and this stage refuses it", node)
				}
			}
		})
	}
}

// TestEveryShapeADocumentCanHoldInAKeyPositionIsRecognised is the key path's half of the same
// pair, and the reason refusing an unrecognised key is safe: apart from the alias its table
// names, nothing a configuration can write reaches that refusal either.
func TestEveryShapeADocumentCanHoldInAKeyPositionIsRecognised(t *testing.T) {
	for name, document := range keyShapeDocuments {
		t.Run(name, func(t *testing.T) {
			keys := everyKeyNodeIn(parsedRoot(t, document))
			if len(keys) == 0 {
				t.Fatal("this document produced no key at all, so the row asserts nothing")
			}
			for _, key := range keys {
				if _, recognised := keyTextOf(key); !recognised {
					t.Errorf("%T is written by a document that parses, and this stage refuses it", key)
				}
			}
		})
	}
}

// TestADocumentThatIsOnlyCommentsIsRefusedBeforeStageD is the one comment shape the
// enumeration above cannot hold, recorded here rather than left out of the corpus.
//
// A file whose whole body is a comment parses to an *ast.CommentGroupNode, which stage D does
// not recognise and would refuse as an unsupported *value* shape -- a diagnostic that says
// nothing true about the file. It never arrives, because stage C refuses the file first, and
// that ordering is the only thing standing between the two. Both halves are asserted, so a
// stage C that stopped refusing empty documents fails here by name rather than surfacing as a
// nonsense diagnostic about a shape.
func TestADocumentThatIsOnlyCommentsIsRefusedBeforeStageD(t *testing.T) {
	const document = "# a file holding nothing but this\n"

	if _, recognised := valuePositionsOf(parsedRoot(t, document)); recognised {
		t.Error("stage D now recognises a comment-only body; it belongs in the enumeration above")
	}

	root, diags := parseDocument(newSource(interpolationFixture, []byte(document)))
	if root != nil {
		t.Errorf("stage C passed a comment-only body of %T to stage D", root)
	}
	if len(diags) != 1 {
		t.Fatalf("stage C returned %d diagnostics %q, want the one refusing an empty file", len(diags), messagesOf(diags))
	}
}

// everyValueNodeIn is each node the recursion would reach through a value edge, including
// the root, so a shape nested inside a recognised container is examined too.
func everyValueNodeIn(root ast.Node) []ast.Node {
	var found []ast.Node
	descendValuePositions(root, func(node ast.Node) { found = append(found, node) })
	return found
}

// everyKeyNodeIn is each key the recursion would visit, so a key nested inside a recognised
// container is examined too -- which is the only way a merge key is reached at all.
func everyKeyNodeIn(root ast.Node) []ast.Node {
	var found []ast.Node
	descendValuePositions(root, func(node ast.Node) {
		found = append(found, keyPositionsOf(node)...)
	})
	return found
}

// descendValuePositions calls visit on root and on every node beneath it that this stage
// reaches through a value edge. It is substituteValuesUnder's descent without the
// substituting, written once because both enumerations above ask about that same set.
//
// It stops where the stage stops: an unrecognised shape is where the recursion refuses
// rather than descends, so a test built on this cannot claim coverage the stage does not
// have.
func descendValuePositions(root ast.Node, visit func(ast.Node)) {
	visit(root)

	values, recognised := valuePositionsOf(root)
	if !recognised {
		return
	}
	for _, value := range values {
		descendValuePositions(value, visit)
	}
}
