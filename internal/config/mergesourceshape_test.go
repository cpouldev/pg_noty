package config

import (
	"maps"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// The shapes a merge source can be written as, enumerated, and the reader that looks past the
// wrappers among them.
//
// A merge source is a value position of its own, so it admits every node property YAML permits
// on a value. The enumeration lives here rather than beside the plain value shapes because this
// step introduced the position; both are joined below, so a test quantified over "a shape a
// document can write" cannot see one and miss the other.

// mergeSourceShapeDocuments is every spelling a `<<` can point at, obtained by parsing the YAML
// that produces it rather than by naming a type. The rows cover the arms reduceValue and
// beneathNodeProperties switch on -- an alias, an anchor, a tag, a mapping written in place, a
// sequence of any of those -- plus the two placements that make the anchor table's ordering
// observable.
//
// The merge type is "a mapping, or a sequence of mappings", and alias resolution is transparent:
// the one-or-many question is therefore asked of what the source *resolved to*, so a name given
// to a list of mappings is as legal as the list written out. The last three rows are that
// crossing -- a list reached through an alias, through a tag, and through both -- and they are
// the rows that fail when the question is asked of the node as written.
var mergeSourceShapeDocuments = map[string]string{
	"merge of a single anchor":                     "a: &a {p: 1}\nvalue:\n  <<: *a\n",
	"merge of two anchors":                         "a: &a {p: 1}\nb: &b {q: 2}\nvalue:\n  <<: [*a, *b]\n",
	"merge of a mapping written in place":          "value:\n  <<: {a: 1}\n",
	"merge of an anchored mapping":                 "value:\n  <<: &m {a: 1}\n",
	"merge of a tagged mapping":                    "value:\n  <<: !!map {a: 1}\n",
	"merge of a sequence of anchored mappings":     "value:\n  <<: [&a {p: 1}, &b {q: 2}]\n",
	"merge of an alias to a tagged mapping":        "base: &base !!map {p: 1}\nvalue:\n  <<: *base\n",
	"merge of an anchor a merge source declares":   "value:\n  <<: &m {a: 1}\nother: *m\n",
	"merge of an anchor holding a tagged sequence": "base: &base !!seq [1]\nvalue:\n  <<: {inner: *base}\n",
	"merge of an alias to a sequence of mappings": "a: &a {p: 1}\nb: &b {q: 2}\n" +
		"pair: &pair [*a, *b]\nvalue:\n  <<: *pair\n",
	"merge of a tagged sequence of mappings":            "value:\n  <<: !!seq [{p: 1}, {q: 2}]\n",
	"merge of an alias to a tagged sequence of anchors": "a: &a {p: 1}\npair: &pair !!seq [*a]\nvalue:\n  <<: *pair\n",
}

// everyValueShapeDocument is both enumerations together: the shapes a plain value position holds
// and the shapes a merge source holds. Every property quantified over "a shape a document can
// write in a value position" ranges over this rather than over either map, because a merge source
// is one of those positions and reading only the first is how a spelling reduced in one position
// and refused in the other stays invisible.
func everyValueShapeDocument() map[string]string {
	every := make(map[string]string, len(valueShapeDocuments)+len(mergeSourceShapeDocuments))
	maps.Copy(every, valueShapeDocuments)
	maps.Copy(every, mergeSourceShapeDocuments)
	return every
}

// TestEveryNodePropertyAMergeSourceAdmitsIsReadThroughToTheMapping covers what a `<<` may point
// at: an anchor and a tag are properties *of* the mapping beneath them, so a position that
// asserts the mapping shape without reading past them tells an author their mapping is not a
// mapping.
//
// The rows are one enumeration rather than a table beside a stray sibling case, because they
// answer one question: refusing a source that is not an alias must not be the answer to every
// source that is not a bare mapping either.
func TestEveryNodePropertyAMergeSourceAdmitsIsReadThroughToTheMapping(t *testing.T) {
	spellings := map[string]string{
		"a mapping written in place":   "use:\n  <<: {inherited: merged}\n  own: written\n",
		"an anchored mapping":          "use:\n  <<: &m {inherited: merged}\n  own: written\n",
		"a tagged mapping":             "use:\n  <<: !!map {inherited: merged}\n  own: written\n",
		"an anchored tagged mapping":   "use:\n  <<: &m !!map {inherited: merged}\n  own: written\n",
		"an alias to a tagged mapping": "base: &base !!map {inherited: merged}\nuse:\n  <<: *base\n  own: written\n",
		"a sequence of anchored mappings": "use:\n  <<: [&a {inherited: merged}, &b {second: merged}]\n" +
			"  own: written\n",
		"an alias to a sequence of mappings": "a: &a {inherited: merged}\npair: &pair [*a]\n" +
			"use:\n  <<: *pair\n  own: written\n",
		"a tagged sequence of mappings": "use:\n  <<: !!seq [{inherited: merged}]\n  own: written\n",
	}

	for name, document := range spellings {
		t.Run(name, func(t *testing.T) {
			used := decodedDocument(t, normalized(t, document))["use"].(map[string]any)

			if used["inherited"] != "merged" || used["own"] != "written" {
				t.Errorf("use = %#v, want the merged key and the direct one", used)
			}
		})
	}
}

// TestBothNodePropertiesAreReadOffASourceHoweverTheyAreNested asserts beneathNodeProperties over
// both its arms directly, because a document reaches only one of them: reduceValue has already
// unwrapped an anchor by the time a source's shape is asserted, so the anchor arm is the one no
// document can reach today.
//
// Asserting it here keeps the position closed over what the grammar permits rather than over the
// order two reductions happen to run in, so moving the peel or the reduction cannot quietly
// reintroduce a mapping being told it is not one.
func TestBothNodePropertiesAreReadOffASourceHoweverTheyAreNested(t *testing.T) {
	written := map[string]string{
		"no property at all":     "value: {a: 1}\n",
		"a tag":                  "value: !!map {a: 1}\n",
		"an anchor":              "value: &m {a: 1}\n",
		"an anchor around a tag": "value: &m !!map {a: 1}\n",
	}

	for name, document := range written {
		t.Run(name, func(t *testing.T) {
			held := firstEntryOf(t, parsedRoot(t, document)).Value

			if _, isMapping := beneathNodeProperties(held).(*ast.MappingNode); !isMapping {
				t.Errorf("%T reads through to %T, want the mapping the author wrote",
					held, beneathNodeProperties(held))
			}
		})
	}
}

// TestGoccyRefusesToParseAnAliasCarryingANodeProperty pins the fact that lets two positions in this
// stage read an element as written rather than through a peel: the alias arms of mergeSourcesOf and
// of the operations fold.
//
// YAML gives an alias no properties to carry, so those are the one position with nothing to read
// past. Were a later version to admit `!!map *a`, a wrapped alias would slip past both arms --
// and an unresolved one inside a merge list would then collect a second diagnostic about the list
// that held it, which is the multiplicity the arms exist to prevent.
func TestGoccyRefusesToParseAnAliasCarryingANodeProperty(t *testing.T) {
	properties := map[string]struct{ refused, accepted string }{
		"a tag on an alias": {
			refused:  "a: &a {p: 1}\nuse: !!map *a\n",
			accepted: "a: &a {p: 1}\nuse: *a\n",
		},
		"an anchor on an alias": {
			refused:  "a: &a {p: 1}\nuse: &b *a\n",
			accepted: "a: &a {p: 1}\nuse: *a\n",
		},
		"a tagged alias inside a merge list": {
			refused:  "a: &a {p: 1}\nuse:\n  <<: [!!map *a]\n",
			accepted: "a: &a {p: 1}\nuse:\n  <<: [*a]\n",
		},
	}

	for name, spelling := range properties {
		t.Run(name, func(t *testing.T) {
			// The accepting half first, so the refusal is attributed to the property rather than
			// to anything else about the document.
			if root, diags := parseDocument(newSource(interpolationFixture, []byte(spelling.accepted))); root == nil {
				t.Fatalf("the same document without the property does not parse either (%q), so this "+
					"row attributes the refusal to the wrong thing", messagesOf(diags))
			}
			if root, _ := parseDocument(newSource(interpolationFixture, []byte(spelling.refused))); root != nil {
				t.Errorf("the document parsed to %T; an alias can now carry a property, so every "+
					"position reading one as written needs a peel", root)
			}
		})
	}
}

// TestGoccyReportsATaggedValueOneColumnBeforeItBegins pins the accounting a tagged source's
// reported column depends on. It is the parser's, not this package's: `value: !!str x` puts `x`
// at rune 14 and the parser reports 13, the separating space. Nothing corrects it, because
// correcting it would mean a second column derivation beside position.go's (ADR-4).
func TestGoccyReportsATaggedValueOneColumnBeforeItBegins(t *testing.T) {
	const document = "value: !!str x\n"

	tagged := firstEntryOf(t, parsedRoot(t, document)).Value.(*ast.TagNode)

	if got := parserReportedColumn(tagged.Value.GetToken()); got != 13 {
		t.Fatalf("the tagged value's column is %d, want 13; goccy's tag accounting changed and "+
			"the merge-source columns derived from it are now wrong", got)
	}
}
