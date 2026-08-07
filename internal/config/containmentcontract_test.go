package config

import (
	"strings"
	"testing"
)

// This file answers for the containment grid's own preconditions: which plants still hide a secret
// where their layout wrote one, and whether the filter that answers it can fire too widely.
//
// It exists because the fuzzer minted `testdata/fuzz/FuzzRenderedTextNeverQuotesASecret/
// 876b6732f796486a`, whose tail opens with `]`. Written into the layout the entry selected when it
// was minted, that closer closes a flow sequence the layout deliberately left unclosed, so the
// document parses, the secret's remaining bytes land in a comment the parser placed outside every
// sensitive value, and the marker search reported a leak the redactor had not committed. The entry
// is kept and its finding answered rather than deleted.
//
// What the two tests below assert is that analysis, over the entry's own tail and a layout named
// here: the entry's stored ordinal no longer selects that layout, and cannot be made to again,
// because the nine entries were minted at three different table sizes. Neither test therefore runs
// the entry, and neither claims to. The entry itself is run -- its own tail against its own two
// ordinals -- by TestEveryCommittedCorpusEntryLeaksNothingThroughItsOwnSelection, and its tail is
// crossed with every layout by the seed grid, which is what keeps the finding reachable.

// closerlessTail is theClosingTail with the one byte that causes the drift removed -- the nearest
// input that must stay in contract.
const (
	closerlessTail   = "#\n#0"
	unclosedFlowSeq  = "an unclosed flow sequence after a closed quoted element"
	closingTailOwner = "876b6732f796486a"
)

// theClosingTail is corpus entry 876b6732f796486a's own tail, read from the manifest rather than
// written out a second time, so the constant cannot drift away from the bytes on disk.
func theClosingTail(t *testing.T) string {
	t.Helper()

	tail, declared := corpusMintedTails[closingTailOwner]
	if !declared {
		t.Fatalf("no tail is declared for corpus entry %s, so the analysis below has no subject",
			closingTailOwner)
	}
	return tail
}

// TestAGeneratedTailThatClosesItsContainerLeavesItsContract pins both sides of the filter the fuzz
// target applies, so a filter widened to swallow the whole grid fails here rather than passing
// vacuously.
//
// The filter is insideASensitiveExtent, and the assertion drives that function rather than a second
// expression of it. It used to be the branch check, which was too coarse: a tail can leave a plant's
// contract without changing the branch at all, which is what the closing tail does and why the
// payload had to be narrowed instead.
func TestAGeneratedTailThatClosesItsContainerLeavesItsContract(t *testing.T) {
	layout := layoutNamed(t, unclosedFlowSeq)

	// The second line's opening marker: the one the corpus entry's failure named.
	watched := leakMarker(2, "HEAD")

	for _, tc := range []struct {
		name       string
		tail       string
		inContract bool
	}{
		{name: "the corpus entry's tail, whose ] closes the sequence", tail: theClosingTail(t)},
		{name: "the same tail with its closer removed", tail: closerlessTail, inContract: true},
		{name: "an ordinary tail", tail: "plain\nsecond line", inContract: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planted := documentsHiding(secretMarkedOnEveryLine(tc.tail), layout, keySpellings[0])[0]
			text := newSource("listeners.yaml", []byte(planted.text))

			// This layout leaves a flow sequence open on purpose, so a tail closing it is exactly
			// what makes the document parse. Which contract the target applies follows from that,
			// and both halves are asserted: an unparseable document is the fallback's, whose
			// contract is every marker, and a parsed one is the schema's, which here excludes the
			// marker the closer moved into a comment.
			found, parses := sensitiveExtentsOf(t, text)
			if parses == tc.inContract {
				t.Fatalf("the document parses = %t; the closer this row does or does not write is "+
					"what decides it:\n%s", parses, planted.text)
			}
			if !parses {
				assertNoMarkerSurvives(t, planted, "key-scoped fallback")
				return
			}
			if insideASensitiveExtent(text, found, watched) {
				t.Fatalf("%s is inside a declared-sensitive value, so the corpus entry it was minted "+
					"from records under-redaction after all:\n%s", watched, planted.text)
			}
		})
	}
}

// TestTheClosingTailPlantsItsSecondMarkerOutsideEverySensitiveExtent is the proof that corpus entry
// 876b6732f796486a recorded a drifted fixture and not under-redaction: written into the layout the
// entry selected when it was minted, its tail puts the marker the failure named on a line the parser
// places outside every value the schema declares sensitive. Should a later change bring that line
// inside one, this fails and the finding becomes a leak report again.
//
// The marker is not claimed to survive rendering -- it does not, on this tree, because the layout
// body was re-geometried since. What is asserted is only where the document puts it.
func TestTheClosingTailPlantsItsSecondMarkerOutsideEverySensitiveExtent(t *testing.T) {
	secret := secretMarkedOnEveryLine(theClosingTail(t))
	planted := documentsHiding(secret, layoutNamed(t, unclosedFlowSeq), keySpellings[0])[0]

	text := newSource("listeners.yaml", []byte(planted.text))
	found, parses := sensitiveExtentsOf(t, text)
	if !parses {
		t.Fatalf("the document no longer parses, so it no longer records the drift it was minted for:\n%s",
			planted.text)
	}
	if len(found) == 0 {
		t.Fatalf("the document declares no sensitive value at all, so the claim below is vacuous:\n%s",
			planted.text)
	}

	// The second line's opening marker is the one the failure named; it is derived from the same
	// generator the plant used rather than written out, so renaming a marker cannot leave this
	// test asserting about a string nothing plants.
	watched := leakMarker(2, "HEAD")
	if !strings.Contains(planted.text, watched) {
		t.Fatalf("the plant does not hold %s, so this test watches the wrong marker:\n%s",
			watched, planted.text)
	}
	assertOutsideEverySensitiveExtent(t, text, found, watched)
}

// assertOutsideEverySensitiveExtent names which extent a marker fell inside, where
// insideASensitiveExtent answers only whether one did. The membership question is asked through that
// one reader so the two cannot disagree; what is added here is the diagnosis, and the refusal of a
// marker no line holds -- one the document never received must not pass an extent check by being
// nowhere.
func assertOutsideEverySensitiveExtent(
	t *testing.T,
	text *source,
	found []declaredSensitiveExtent,
	watched string,
) {
	t.Helper()

	line := lineHoldingMarker(text, watched)
	if line == notOnAnyLine {
		t.Fatalf("no line holds %s", watched)
	}
	if !insideASensitiveExtent(text, found, watched) {
		return
	}
	for _, value := range found {
		// Asked of one value at a time through the same reader, so the diagnosis cannot name an
		// extent the claim above did not come from.
		if insideASensitiveExtent(text, []declaredSensitiveExtent{value}, watched) {
			t.Errorf("%s is on line %d, inside %s, the value %q written across lines %d-%d: "+
				"the entry records under-redaction after all",
				watched, line, value.locator, value.text, value.line, value.through)
		}
	}
}

func layoutNamed(t *testing.T, name string) secretLayout {
	t.Helper()

	for _, layout := range secretLayouts {
		if layout.name == name {
			return layout
		}
	}
	t.Fatalf("no containment layout is named %q", name)
	return secretLayout{}
}
