package config

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// This file is the class of author-written runes an author writes between a sensitive key's colon
// and the first token of the value written below it -- the *opener gap*.
//
// The parser anchors a value on its own first token, so `secrets:` holding a list is anchored on
// that list's first `- `. Every reader of the value's extent used to begin there, which put a
// rotation note written above the first item outside the value: the path-aware branch rendered it
// while the key-scoped fallback blanked it, and two branches disagreeing about one input class is
// how the class stayed open.
//
// The two axes below are the class, not the reproduction. The reproduction was a `#` comment one
// line above the first item of a block sequence, and each of those three properties is incidental:
// a bound conditioned on the `#` leaves an anchor property rendering, one conditioned on "the line
// directly above" leaves a longer gap rendering, and one conditioned on the block style leaves a
// flow container written below its key rendering. The rows are the product of the two axes and the
// count is pinned, so a value added to either without a document fails in the enumeration rather
// than as a rendered secret.
//
// A `!!tag` written in the gap is deliberately not a row here, and the reason is measured rather
// than assumed: this library version makes the tag *be* the value, so the node is anchored on the
// tag's own line and the gap is empty. That is a different shape, and it is pinned as one by
// TestGoccyAnchorsAValueOnTheFirstTokenOfItsOwnText.

// openerGapMaterial is what an author can write between a key's colon and the value beneath it.
// Both spellings are here because the fallback branch recognises a comment by its `#` and this
// branch must recognise neither -- the gap is bounded by the two delimiters around it, not by what
// somebody wrote in it.
var openerGapMaterial = []struct {
	name  string
	write func(marked string) string
}{
	{name: "a comment", write: func(marked string) string {
		return "        # " + marked + "\n"
	}},
	// An anchor name carries no space and no flow indicator, which is why the marked text written
	// into it must not either.
	{name: "an anchor property", write: func(marked string) string {
		return "        &" + marked + "\n"
	}},
}

// openerGapContainers is the shape written below the gap. All four are anchored on a token of
// their own below the key, which is what makes a gap possible at all; a value written beside its
// key has no room for one.
//
// The axis is the cross of YAML's two container kinds with its two styles, so both flow shapes are
// written below their key rather than beside it: a flow container written beside its key is the
// no-gap shape, and it is a row of the separation table instead
// (sensitiveextentseparation_test.go). The flow mapping was the one cell of that cross nobody
// wrote, and an unwritten cell is a cell whose containment rests on a reviewer constructing it.
var openerGapContainers = []struct {
	name  string
	write func(secret string) string
}{
	{name: "a block sequence", write: func(secret string) string {
		return "        - " + secret + "\n        - " + secret + "\n"
	}},
	{name: "a block mapping", write: func(secret string) string {
		return "        first: " + secret + "\n        second: " + secret + "\n"
	}},
	{name: "a flow sequence written below its key", write: func(secret string) string {
		return "        [" + secret + ", " + secret + "]\n"
	}},
	{name: "a flow mapping written below its key", write: func(secret string) string {
		return "        {first: " + secret + ", second: " + secret + "}\n"
	}},
}

// openerGapWatchedText is what every cell writes into the gap. It holds no space and no flow
// indicator so that one string serves the comment spelling and the anchor spelling alike.
const openerGapWatchedText = "rotated-2026-07-14-keep-the-old-one-until-friday"

// blockOpenerGapLayouts is the product, as layouts of the containment grid.
//
// It reaches secretLayouts rather than living in a table of its own because
// FuzzRenderedTextNeverQuotesASecret indexes secretLayouts: a case list the fuzzer cannot index is
// a case list the fuzzer is blind to, whatever else runs it. The gap material is a watched
// physical tail, so documentsHiding contributes its markers to every planted document and the
// oracle searches rendered output for them beside the secret's own.
func blockOpenerGapLayouts() []secretLayout {
	layouts := make([]secretLayout, 0, len(openerGapMaterial)*len(openerGapContainers))
	for _, material := range openerGapMaterial {
		for _, container := range openerGapContainers {
			layouts = append(layouts, secretLayout{
				name:        material.name + " above " + container.name,
				body:        openerGapBody(material.write, container.write),
				watchedTail: textMarkedAtBothEnds("OPENER-GAP", openerGapWatchedText),
			})
		}
	}
	return layouts
}

// openerGapBody writes one cell: the sensitive key, the author's runes beneath it, and the value
// beneath those.
func openerGapBody(
	material func(marked string) string,
	container func(secret string) string,
) func(secret string, key keySpelling) string {
	return func(secret string, key keySpelling) string {
		return signingSecrets("      " + key.write("secrets") + "\n" +
			material(watchedTailSlot) + container(yamlQuoted(secret)))
	}
}

// openerGapCellNames is the product in the order the two axes declare their values, so a cell
// nobody wrote fails by name.
func openerGapCellNames() []string {
	crossed := make([]string, 0, len(openerGapMaterial)*len(openerGapContainers))
	for _, material := range openerGapMaterial {
		for _, container := range openerGapContainers {
			crossed = append(crossed, material.name+" above "+container.name)
		}
	}
	return crossed
}

// aGapMarkerIsWrittenAboveTheValuesOwnStart reports whether one planted document really writes its
// watched marker above the line the sensitive value's own text begins on -- the precondition every
// cell of this family declares. It is measured from the parsed document rather than trusted from
// the layout, because a generated tail can restructure one.
func aGapMarkerIsWrittenAboveTheValuesOwnStart(document, marker string) (bool, string) {
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		return false, "the document does not parse"
	}
	_ = normalize(text, root)

	marked := lineHoldingMarker(text, marker)
	if marked == notOnAnyLine {
		return false, "no line holds " + marker
	}
	// writtenStartOf, not the oracle's extent: the extent is the answer under test and grows to
	// cover the gap, so reading the precondition from it would make this claim self-satisfying.
	for _, node := range declaredSecretsValuesOf(root) {
		if marked < writtenStartOf(text, node).line {
			return true, ""
		}
	}
	return false, "no declared secrets value begins on a line below " + marker
}

// declaredSecretsValuesOf is every node this document writes at a `secrets` locator the table
// declares sensitive, read through the oracle's own locator expansion.
func declaredSecretsValuesOf(root ast.Node) []ast.Node {
	var found []ast.Node
	for _, locator := range sensitiveSchemaLocators() {
		if !strings.HasSuffix(locator, "secrets") {
			continue
		}
		for _, written := range concreteLocatorsOf(root, locator) {
			if node, exists := nodeWrittenAt(root, written); exists {
				found = append(found, node)
			}
		}
	}
	return found
}
