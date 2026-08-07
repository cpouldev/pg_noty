package config

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// This file is the two ways one mapping can name one thing twice. They are one walk over two
// equivalences, because "which of these keys repeats an earlier one, and which earlier one" is the
// same question whether sameness means the same key or the same header.
//
// **R42's completeness is this stage's, and this is the answer to the question Step 4's
// Implementation Note 17 left open.** The parser's own duplicate detection runs at stage B and is
// complete for every spelling of a *textual* key -- measured on v1.19.2: `insert` written again as
// `'insert'`, `"insert"`, `!!str insert` or `? insert` is caught there, and two distinct keys
// written in those spellings are not falsely accused. What it does not catch is two *renderings*
// of one non-textual value. `16:` and `0x10:` are the integer sixteen written twice and
// `parseDocument` reports nothing; the mapping then reaches Step 6's decoder holding one key twice
// and fails there with `duplicate key "16"` and no position at all. On a free-form header mapping
// nothing else would have reported it either, so the document would be one stage F called clean.
// Stage F is where the answer belongs: it is the last stage that walks the document's own
// mappings, and keyIdentity is already this package's answer to when two keys are one key.
//
// R20 is the same walk under a different equivalence: HTTP field names are case-insensitive, so
// `X-Trace` and `x-trace` are one header written twice and would otherwise become two headers of
// one name on the wire.

// repeat is one key that names something an earlier key already named.
type repeat struct {
	at    ast.MapKeyNode
	first Positioned
}

// anotherSpellingOfOneKey is what the second of two renderings of one key is told. It names the
// first occurrence's line, which is the convention the parser's own duplicate-key diagnostic sets
// at stage B, so an author meets one wording for one condition however it was found.
func anotherSpellingOfOneKey(first int) fault {
	return fault{
		message: "this key is another spelling of the one already written on line " + strconv.Itoa(first),
		hint:    "two spellings of one value are one key, so write it once",
	}
}

// headerWrittenTwice is R20's wording, naming the first occurrence's line for the same reason.
func headerWrittenTwice(name string, first int) fault {
	return fault{
		message: "header " + strconv.Quote(name) + " differs only in case from the one on line " + strconv.Itoa(first),
		hint:    "HTTP field names are case-insensitive, so the two would be one header on the wire",
	}
}

// refuseARepeatedName is R42's completeness over the mappings this stage walks.
//
// It stops the run for the reason a wrong node kind does: a mapping holding one key twice is not a
// mapping the decoder can read, so the stages after this one have nothing to work with.
func (p *shapePass) refuseARepeatedName(ordered []*ast.MappingValueNode) {
	for _, repeated := range repeatsIn(p.src, ordered, oneKey) {
		p.reportAs(R42, repeated.at, anotherSpellingOfOneKey(repeated.first.Line()))
		p.undecodable = true
	}
}

// refuseNamesDifferingOnlyInCase is R20. It does not stop the run: two headers of one name are a
// delivery mistake rather than a shape the decoder cannot read.
func (p *shapePass) refuseNamesDifferingOnlyInCase(ordered []*ast.MappingValueNode) {
	for _, repeated := range repeatsIn(p.src, ordered, oneHeaderName) {
		name, _ := keyTextOf(repeated.at)
		p.reportAs(R20, repeated.at, headerWrittenTwice(name, repeated.first.Line()))
	}
}

// oneKey is R42's equivalence: what makes two keys the same key (keyidentity.go). Every key names
// itself, so no key is exempt.
func oneKey(key ast.MapKeyNode) (string, bool) { return keyIdentity(key), true }

// oneHeaderName is R20's equivalence: a field name folded to one case.
//
// A key carrying no text names no header and is exempt. An empty name is R19's to refuse (Step 9),
// and a key the parser read as a number or a boolean is not a field name at all -- while both are
// still keys, so R42's equivalence above sees them. Without the exemption every such key would
// fold onto the empty name and be reported twice over, once as a repeated key and once as a
// repeated header.
func oneHeaderName(key ast.MapKeyNode) (string, bool) {
	text, readable := keyTextOf(key)
	if !readable || text == "" {
		return "", false
	}
	return strings.ToLower(text), true
}

// entriesInWrittenOrder is a mapping's entries in the order their keys were written.
//
// "Written" rather than "held" is a correctness requirement rather than a refinement: after stage
// E's merge expansion a mapping's entries are no longer in document order, because an inherited
// entry carries the anchor's own earlier line while sitting last in the slice. A first-occurrence
// rule reading the slice would name the wrong line on every merged mapping.
//
// The order is taken on a copy. The slice belongs to the document and the decoder reads it, so
// sorting it in place would reorder the configuration to answer a question about it.
//
// It is computed once per mapping and handed to both refusals above, because they need the
// identical ordering and a header mapping is asked both of their questions.
func entriesInWrittenOrder(src *source, mapping *ast.MappingNode) []*ast.MappingValueNode {
	ordered := slices.Clone(mapping.Values)
	slices.SortStableFunc(ordered, func(a, b *ast.MappingValueNode) int {
		return comparePositions(positionOf(src, a.Key), positionOf(src, b.Key))
	})
	return ordered
}

// repeatsIn is every key of a mapping that names something an earlier key already named, where what
// a key names is the caller's equivalence and "earlier" is the order it was handed.
func repeatsIn(src *source, ordered []*ast.MappingValueNode, naming func(ast.MapKeyNode) (string, bool)) []repeat {
	var repeated []repeat
	firstAt := make(map[string]Positioned, len(ordered))

	for _, entry := range ordered {
		named, names := naming(entry.Key)
		if !names {
			continue
		}

		if at, already := firstAt[named]; already {
			repeated = append(repeated, repeat{at: entry.Key, first: at})
			continue
		}
		firstAt[named] = positionOf(src, entry.Key)
	}
	return repeated
}

// comparePositions orders two positions by where they are written.
//
// It is deliberately not compareByPosition, which orders finished diagnostics by four keys
// including their locator and their message. This one answers only which of two tokens the author
// wrote first, and a diagnostic order applied to that question would sort by fields a token does
// not have.
func comparePositions(a, b Positioned) int {
	return cmp.Or(cmp.Compare(a.Line(), b.Line()), cmp.Compare(a.Col(), b.Col()))
}
