package config

import (
	"strings"
	"testing"
)

// This file is the assertion half of the opener-gap class; containmentopenergap_test.go holds the
// grid it runs. It also pins the two library facts the production derivation rests on, so an
// upgrade fails here by name rather than as a rendered secret.

// TestEveryCellOfTheBlockOpenerGapGridIsContainedOnBothBranches runs the product on both of D3's
// branches, with the branch measured from the document's own bytes rather than declared by the
// layout. The class is a *disagreement* between the branches, so a subject that ran only one of
// them could not see it.
func TestEveryCellOfTheBlockOpenerGapGridIsContainedOnBothBranches(t *testing.T) {
	layouts := blockOpenerGapLayouts()
	if len(layouts) == 0 {
		t.Fatal("the opener-gap family generates no layout, so every claim below is vacuous")
	}

	for _, layout := range layouts {
		for _, spelling := range keySpellings {
			for _, planted := range documentsHiding(secretMarkedOnEveryLine("plain"), layout, spelling) {
				t.Run(layout.name+", a "+spelling.name+" key, "+planted.branch, func(t *testing.T) {
					assertNothingLeaks(t, planted)
				})
			}
		}
	}
}

// TestTheBlockOpenerGapGridIsCrossedOnBothAxes keeps the family closed over its two axes: a
// material or a container added to either without its cell fails here rather than narrowing a
// property that reads as universal.
func TestTheBlockOpenerGapGridIsCrossedOnBothAxes(t *testing.T) {
	want := len(openerGapMaterial) * len(openerGapContainers)
	if want == 0 {
		t.Fatal("an axis of the opener-gap grid is empty, so the product covers nothing")
	}

	named := openerGapCellNames()
	if len(named) != want {
		t.Fatalf("crossing produced %d cells for %d material x container cells", len(named), want)
	}

	written := make(map[string]bool, len(named))
	for _, layout := range blockOpenerGapLayouts() {
		written[layout.name] = true
	}
	for _, cell := range named {
		if !written[cell] {
			t.Errorf("no layout for the %q cell; every material is writable above every container",
				cell)
		}
	}
}

// TestEveryBlockOpenerGapCellReallyWritesAboveTheValuesOwnStart asserts the precondition every cell
// declares. A cell whose material stopped sitting above the value's own first token would still be
// contained -- by the machinery that was already there -- and would report coverage of a class it
// never entered.
func TestEveryBlockOpenerGapCellReallyWritesAboveTheValuesOwnStart(t *testing.T) {
	for _, layout := range blockOpenerGapLayouts() {
		for _, planted := range documentsHiding(secretMarkedOnEveryLine("plain"), layout, keySpellings[0]) {
			if !planted.pathAware {
				continue // only a document that parses has a written start to compare against
			}
			t.Run(layout.name, func(t *testing.T) {
				for _, marker := range layout.watchedTail.markers {
					above, why := aGapMarkerIsWrittenAboveTheValuesOwnStart(planted.text, marker)
					if !above {
						t.Errorf("%s: %s, so this cell no longer plants in the opener gap:\n%s",
							marker, why, planted.text)
					}
				}
			})
		}
	}
}

// TestAGapOfSeveralLinesIsContainedWholeOnBothBranches is the row that keeps the bound off "the line
// directly above the first item". Three lines of gap, of three different shapes, and the farthest from
// the value is watched as well as the nearest -- so a repair that blanks only the line it met fails
// naming the marker it left.
//
// The blank line is written deliberately: its own bytes can hide nothing, and blanking it would
// replace an empty line with a placeholder, so it is the one shape the gap walk must skip.
func TestAGapOfSeveralLinesIsContainedWholeOnBothBranches(t *testing.T) {
	body := signingSecrets("      secrets:\n" +
		"        # " + leakSentinel + "FARTHEST\n" +
		"\n" +
		"        &" + leakSentinel + "NEAREST\n" +
		"        - " + leakSentinel + "ITEM\n")

	for _, planted := range plantedOnBothBranches("a three-line opener gap", body,
		[]string{leakSentinel + "FARTHEST", leakSentinel + "NEAREST", leakSentinel + "ITEM"}) {
		t.Run(planted.branch, func(t *testing.T) {
			assertNothingLeaks(t, planted)
		})
	}
}

// TestTheOpenerGapWatchedTextCarriesNoAnchorBreakingRune keeps the grid's one string usable in both
// spellings it is written in. An anchor name ends at a space or a flow indicator, so a marker
// holding one would end the anchor early and the cell would plant a shape other than its name.
func TestTheOpenerGapWatchedTextCarriesNoAnchorBreakingRune(t *testing.T) {
	written := textMarkedAtBothEnds("OPENER-GAP", openerGapWatchedText).text

	for _, breaking := range []string{" ", "\t", "[", "]", "{", "}", ",", "#"} {
		if strings.Contains(written, breaking) {
			t.Errorf("the watched gap text holds %q, which ends an anchor name: %q",
				breaking, written)
		}
	}
}
