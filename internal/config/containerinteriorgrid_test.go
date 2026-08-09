package config

import "testing"

// This file crosses the two axes containerinterior_test.go's rows name in prose.
//
// Those rows say they vary "the container style, and where inside the container the runes sit",
// which claims a product. They were written by transcribing the flow spellings into block ones, and
// the two positions with no obvious block translation were dropped rather than translated: block
// style took only the *between* value. The uncovered cells were where the defect lived -- the
// interior's last line was derived from the container's children, so an author's note written after
// the last element was outside a scan that read as if it covered the whole interior, in flow style
// as well as block.
//
// The axes are values here, the cells are their product, and the count is pinned, so a style or a
// position added to either axis fails in this enumeration rather than as a rendered secret.

// containerStyles is how YAML delimits a collection: a flow container writes brackets the parser
// reports, a block container writes indentation and no closer at all.
var containerStyles = []string{"flow", "block"}

// interiorPositions is where inside a container author-written runes can sit relative to its
// elements. Every one of them is inside a value the table declares secret in full, so every one of
// them is that value's to hide.
//
// The fourth position is outside the container's own *text* and still inside the value: a container
// written below its key is anchored on its own first token, and the lines an author wrote between
// the key's colon and that token belong to the block the colon opened. Both styles have a spelling
// of it -- a flow container can be written below its key as readily as a block one -- so it is a
// value of this axis rather than a cell bolted onto the product.
var interiorPositions = []string{
	"above the container's own written start",
	"before the first element",
	"between two elements",
	"after the last element",
}

// containerInteriorCells is the `signing.secrets` declaration each cell of the product writes. Each
// plants a marker on the element *and* on the author-written runes, so a repair that replaces the
// elements and renders the runes between them fails naming the second marker.
var containerInteriorCells = map[string]string{
	// A note written between the key's colon and the container's own opening bracket. The parser
	// anchors the value on that bracket, so a bound taken from the container's own text begins
	// below the note -- which is why both cells of this position rendered it until the extent
	// learned to reach back to the block the key opened (writtenstart.go).
	"flow/above the container's own written start": "      secrets:\n        # " +
		leakSentinel + "NOTE\n        [" + leakSentinel + "FIRST, " + leakSentinel + "SECOND]\n",

	"flow/before the first element": "      secrets: [&" + leakSentinel + "ANCHOR " +
		leakSentinel + "ONLY]\n",

	"flow/between two elements": "      secrets: [" + leakSentinel + "FIRST,&" +
		leakSentinel + "ANCHOR " + leakSentinel + "SECOND]\n",

	// A comment written past the last element and before the closer the parser reports. The
	// members-derived bound made this line the interior's last, which stopped the scan after the
	// last child's own text and left the comment outside it.
	"flow/after the last element": "      secrets: [" + leakSentinel + "FIRST,\n" +
		"        " + leakSentinel + "SECOND, # " + leakSentinel + "NOTE\n        ]\n",

	// The rotation note written above the list's first item marker, which is where the parser
	// anchors a block sequence. This is the cell the test below used to pin as live behaviour.
	"block/above the container's own written start": "      secrets:\n        # " +
		leakSentinel + "NOTE\n        - " + leakSentinel + "FIRST\n        - " +
		leakSentinel + "SECOND\n",

	"block/before the first element": "      secrets:\n        - &" + leakSentinel + "ANCHOR " +
		leakSentinel + "FIRST\n        - " + leakSentinel + "SECOND\n",

	"block/between two elements": "      secrets:\n        - " + leakSentinel + "FIRST\n" +
		"        # " + leakSentinel + "NOTE\n        - " + leakSentinel + "SECOND\n",

	// The rotation note beside a signing secret: inside the block by YAML's own indentation rule,
	// and past every child the container has.
	"block/after the last element": "      secrets:\n        - " + leakSentinel + "FIRST\n" +
		"        - " + leakSentinel + "SECOND\n        # " + leakSentinel + "NOTE\n",
}

// crossedInteriorCells is the product of the two axes, in the order they declare their values.
func crossedInteriorCells() []string {
	crossed := make([]string, 0, len(containerStyles)*len(interiorPositions))
	for _, style := range containerStyles {
		for _, position := range interiorPositions {
			crossed = append(crossed, style+"/"+position)
		}
	}
	return crossed
}

// TestTheContainerInteriorGridIsCrossedOnBothAxes keeps the case list closed over both axes: a value
// added to either without a document to go with it fails here, by the name of the cell nobody wrote.
func TestTheContainerInteriorGridIsCrossedOnBothAxes(t *testing.T) {
	crossed := crossedInteriorCells()

	if want := len(containerStyles) * len(interiorPositions); len(crossed) != want {
		t.Fatalf("crossing produced %d cells for %d style x position cells", len(crossed), want)
	}
	if len(containerInteriorCells) != len(crossed) {
		t.Errorf("%d documents for %d cells of the product %v x %v",
			len(containerInteriorCells), len(crossed), containerStyles, interiorPositions)
	}
	for _, cell := range crossed {
		if _, written := containerInteriorCells[cell]; !written {
			t.Errorf("no document for the %q cell; every style writes runes at every position",
				cell)
		}
	}
}

// TestEveryCellOfTheContainerInteriorGridIsContained runs the product. Before the interior's last
// line was bounded by the container's own delimiter, the two after-the-last-element cells rendered
// their planted note verbatim on the path-aware branch; before its first line was bounded by the
// container's *opening* delimiter, so did the two above-the-written-start cells.
func TestEveryCellOfTheContainerInteriorGridIsContained(t *testing.T) {
	for _, cell := range crossedInteriorCells() {
		written, exists := containerInteriorCells[cell]
		if !exists {
			continue // named by TestTheContainerInteriorGridIsCrossedOnBothAxes
		}

		t.Run(cell, func(t *testing.T) {
			source := "version: 1\n" + signingSecrets(written)

			markers := markersIn(source)
			if len(markers) < 2 {
				t.Fatalf("the cell plants %d markers, want the element's and the author's runes':\n%s",
					len(markers), source)
			}
			assertEveryMarkerIsRedacted(t, source, markers...)
		})
	}
}

// noteAboveTheFirstItem writes a comment between a sensitive key and the first item of the block
// container it holds -- above the container's own written start rather than inside it. It returns
// the declaration alone so the test below can pair it onto both branches through the one pairing
// every containment grid uses.
func noteAboveTheFirstItem() (body, watched string) {
	return signingSecrets(
			"      secrets:\n        # " + leakSentinel + "NOTE\n" +
				"        - " + leakSentinel + "FIRST\n        - " + leakSentinel + "SECOND\n"),
		leakSentinel + "NOTE"
}

// TestANoteAboveABlockContainersFirstItemIsInsideTheExtentBothReadersRead is the closed form of a
// gap this file used to record as open -- the same test, one word of its name apart, asserting that
// the note was *outside* the extent both readers read.
//
// The parser anchors a block sequence on its first `- `, so a comment written above that marker is
// above the container's writtenStartOf. Both readers used to derive the value's extent from that
// one answer and both placed the comment outside the value: the oracle did not claim it and the
// path-aware branch rendered it, while the key-scoped fallback blanked it -- one input class, two
// answers.
//
// Both of the clauses that recorded the gap are inverted here, which is what closing it had to do
// to this test: the oracle now claims the note, and neither branch renders it. The extent moved and
// the replacement did not -- writtenStartOf still answers where the value's own text begins, and
// only writtenStartReachingItsBlockOpener reaches past it, so no caret and no golden moved with the
// repair.
func TestANoteAboveABlockContainersFirstItemIsInsideTheExtentBothReadersRead(t *testing.T) {
	body, watched := noteAboveTheFirstItem()
	document := "version: 1\n" + body
	text := newSource("listeners.yaml", []byte(document))

	found, parses := sensitiveExtentsOf(t, text)
	if !parses {
		t.Fatalf("the document does not parse, so it does not reach the branch this pins:\n%s", document)
	}
	if !insideASensitiveExtent(text, found, watched) {
		t.Fatalf("the oracle no longer claims %s, so every containment claim about the opener gap "+
			"below passes by not being asked:\n%s", watched, document)
	}

	// Every marker the document plants, not only the note: the elements are what the note shares
	// its extent with.
	for _, planted := range plantedOnBothBranches(
		"a note above a block container's first item", body, markersIn(document)) {
		t.Run(planted.branch, func(t *testing.T) {
			assertNothingLeaks(t, planted)
		})
	}
}
