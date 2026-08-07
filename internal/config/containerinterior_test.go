package config

import (
	"strings"
	"testing"
)

// This file runs the class containerinterior.go names, rather than the reproduction that found it.
//
// The class is: every rune inside a container the table declares secret in full, which none of its
// children claims, is author-written text inside that value. The reproduction was an `&anchor`
// property between two elements of a flow sequence **closing on a later line**, and the repair that
// turned it green tested the closing line -- a property of the reproduction. The rows below differ
// from it in one incidental property at a time: **the shape of the author's runes** -- an anchor
// name, a tag suffix, a comment -- **and whether the container closes on a later line**. Each of
// them rendered a planted marker verbatim before the repair.
//
// The container style crossed with where inside the container the runes sit is the *other* pair of
// axes, and it is crossed as a product in containerinteriorgrid_test.go rather than claimed in prose
// here: this list took the block spelling of only one of the three positions, and both cells it
// dropped were live defects.

// containerInteriorRows write the `signing.secrets` declaration, whose whole value the table declares
// secret. Every row plants at least two markers, and the assertion below reads them out of the document
// it built rather than being handed a list, so a row that stops planting one cannot quietly assert about
// a string it never wrote.
var containerInteriorRows = []struct {
	name    string
	written string
}{
	{
		name: "an anchor between two elements, the container closing on a later line",
		written: "      secrets: [" + leakSentinel + "FIRST,&" + leakSentinel + "ANCHOR\n" +
			"        " + leakSentinel + "SECOND]\n",
	},
	{
		name: "an anchor between two elements, the container closing on its own line",
		written: "      secrets: [" + leakSentinel + "FIRST,&" + leakSentinel + "ANCHOR " +
			leakSentinel + "SECOND]\n",
	},
	{
		name:    "an anchor before the only element",
		written: "      secrets: [&" + leakSentinel + "ANCHOR " + leakSentinel + "ONLY]\n",
	},
	{
		name: "a tag between two elements, the container closing on a later line",
		written: "      secrets: [" + leakSentinel + "FIRST, !!str\n" +
			"        " + leakSentinel + "SECOND]\n",
	},
	{
		name:    "a tag before the second element, the container closing on its own line",
		written: "      secrets: [" + leakSentinel + "FIRST, !!str " + leakSentinel + "SECOND]\n",
	},
	{
		name: "a comment between two elements, the container closing on a later line",
		written: "      secrets: [" + leakSentinel + "FIRST, # " + leakSentinel + "NOTE\n" +
			"        " + leakSentinel + "SECOND]\n",
	},
	{
		// The block spellings of the same three. No block container reports a closing line at all,
		// so a condition written on one was inert for every row here (sensitiveextent.go).
		name: "an anchor on a block sequence item",
		written: "      secrets:\n        - " + leakSentinel + "FIRST\n" +
			"        - &" + leakSentinel + "ANCHOR " + leakSentinel + "SECOND\n",
	},
	{
		name: "a tag on a block sequence item",
		written: "      secrets:\n        - " + leakSentinel + "FIRST\n" +
			"        - !!str " + leakSentinel + "SECOND\n",
	},
	{
		name: "a comment between two block sequence items",
		written: "      secrets:\n        - " + leakSentinel + "FIRST\n" +
			"        # " + leakSentinel + "NOTE\n        - " + leakSentinel + "SECOND\n",
	},
}

// TestASensitiveContainerHoldingRunesNoChildClaimsIsBlankedWholesale is the class, one row per
// shape of author-written rune and per closing line.
func TestASensitiveContainerHoldingRunesNoChildClaimsIsBlankedWholesale(t *testing.T) {
	for _, row := range containerInteriorRows {
		t.Run(row.name, func(t *testing.T) {
			source := "version: 1\n" + signingSecrets(row.written)

			markers := markersIn(source)
			if len(markers) < 2 {
				t.Fatalf("the row plants %d markers, want the element's and the property's:\n%s",
					len(markers), source)
			}
			assertEveryMarkerIsRedacted(t, source, markers...)
		})
	}
}

// TestASensitiveContainerClaimedByItsChildrenKeepsTheRestOfItsLine is the other side, and it is
// written to cost something.
//
// The bound this replaces wrote `secrets: [ONELINE]` -- a container with no inter-element runes at
// all -- so it could not tell "keep the sibling key beside the container" from "keep runes inside
// it", and a repair scoped as narrowly as one liked still passed it. Both rows here write runes
// between the elements; one row's are the grammar's own and must survive with the sibling key, the
// other's are an author's and must go while that same sibling key still survives.
func TestASensitiveContainerClaimedByItsChildrenKeepsTheRestOfItsLine(t *testing.T) {
	const survives = "algorithm: hmac-sha256"

	for _, row := range []struct {
		name      string
		container string
		// wantSecretsLine is what the rule produces here, derived from the rule rather than
		// recorded from a run. Where every rune between the elements is the grammar's own, each
		// element is replaced where it stands, so the container's own brackets and comma survive
		// around two placeholders. Where one of them is an author's, the value is taken from its
		// opening bracket, so a single placeholder stands for the whole container and the brackets
		// go with it.
		//
		// The discriminator this replaces was `strings.Contains(rendered, "["+redactionPlaceholder)`.
		// Both renderings satisfy it -- `[[redacted]` is equally what an opening bracket followed by
		// a replaced first element looks like -- so it separated the two rows in neither direction.
		wantSecretsLine string
	}{
		{
			name:            "commas and spaces between the elements are the grammar's own",
			container:       "[" + leakSentinel + "FIRST, " + leakSentinel + "SECOND]",
			wantSecretsLine: "      secrets: [" + redactionPlaceholder + ", " + redactionPlaceholder + "]",
		},
		{
			name:            "an anchor between the same two elements is not",
			container:       "[" + leakSentinel + "FIRST,&" + leakSentinel + "ANCHOR " + leakSentinel + "SECOND]",
			wantSecretsLine: "      secrets: " + redactionPlaceholder,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			source := "version: 1\n" + signingSecrets(
				"      secrets: "+row.container+"\n      "+survives+"\n")

			assertEveryMarkerIsRedacted(t, source, markersIn(source)...)
			rendered := renderEveryLineOf(source)
			if !strings.Contains(rendered, survives) {
				t.Errorf("the sibling key %q written on the line below the container was blanked "+
					"with it, so the reach ran past the container's own extent:\n%s",
					survives, rendered)
			}
			assertAQuotedLineIsExactly(t, rendered, row.wantSecretsLine)
		})
	}
}

// assertAQuotedLineIsExactly requires one line of the rendered snippet to be exactly want. A snippet
// writes each quoted line after a `| ` and ends it with a newline, so bounding the search with both
// is what makes this an equality on the line rather than a substring test that a wider or narrower
// replacement on the same line would also satisfy.
func assertAQuotedLineIsExactly(t *testing.T, rendered, want string) {
	t.Helper()

	if !strings.Contains(rendered, "| "+want+"\n") {
		t.Errorf("no quoted line is exactly %q; the replacement covered a different extent\n%s",
			want, rendered)
	}
}
