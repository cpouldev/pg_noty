package config

import "testing"

// This file is what keeps the containment oracle from becoming the thing it audits again.
//
// Two claims, because either alone is satisfiable by an oracle that has drifted back. The
// structural one says the oracle never asks the walk; the behavioural one says the oracle's answer
// is not merely reachable without the walk but actually *differs* from it on a document the suite
// runs.

// theOracleFiles is every file the containment oracle's authority is composed across. It is a set
// rather than a name because the oracle spans four: the entry point the subjects call, the
// extents it derives, the concrete paths it expands a declared locator into, and the table
// positions it reads them from.
//
// Scanning one of them left the claim satisfiable by relocation: sensitiveExtentsOf -- the oracle's
// own entry point -- lives in a file the scan never opened, so reinstating the walk there passed. A
// file added to the oracle and not to this set is caught by
// TestTheOracleFileSetSpansTheOraclesOwnEntryPoint.
var theOracleFiles = []string{
	"containmentextents_test.go",
	"containmentschemaoracle_test.go",
	"containmentschemapaths_test.go",
	"schemapositions_test.go",
}

// theWalkTheOracleAudits is the production entry point whose output the containment subjects judge.
// An oracle that calls it answers "a value the walk found was not replaced", which is true of every
// walk including one that found nothing.
const theWalkTheOracleAudits = "sensitiveValues"

// aNoteAfterTheLastBlockItem writes a rotation note after the last item of `signing.secrets`, whose
// whole value the table declares secret. It is inside that value by YAML's own indentation rule and
// past every child the container has, so no value the walk reports covers it: a sensitiveValue's
// span is its attributed text at its own coordinates, and a block container writes no closing
// indicator for its throughLine to carry. The redactor reaches it by walking the block afterwards
// (sensitivereach.go), which is a decision the walk's output does not record -- and an oracle
// inheriting that output is therefore blind to exactly the bytes this document plants.
//
// The anchor-between-two-elements document this replaces separated the two answers only while the
// walk side was read as *text*. Asserted through the extent mechanism the oracle itself answers in,
// the container-owner value's span runs from the opening bracket for as many runes as the flow
// sequence *renders* to, and that happens to be wide enough to cover the anchor name -- so it stopped
// being a disagreement. Its containment is run by containerInteriorCells.
func aNoteAfterTheLastBlockItem() (document, watched string) {
	return "version: 1\n" + signingSecrets(
			"      secrets:\n        - "+leakSentinel+"FIRST\n"+
				"        - "+leakSentinel+"SECOND\n        # "+leakSentinel+"NOTE\n"),
		leakSentinel + "NOTE"
}

// walkAttributedExtents is what the walk attributes, expressed in the very terms the oracle answers
// in, so the disagreement below is asserted *through the oracle's own mechanism* rather than through
// a second expression of it. Reading the walk side with strings.Contains left the reinstated
// tautology alive: the oracle answers by extent membership, and no assertion exercised that.
//
// A walk value carries a line, the column its text begins at, that text, and the closing line the
// parser reported for it -- so its extent is those runes at those coordinates, through that line or
// its own where none was reported. That mapping is the whole of what a tautological oracle could
// hand back.
func walkAttributedExtents(text *source) []declaredSensitiveExtent {
	var found []declaredSensitiveExtent
	for _, value := range walkAttributedValues(text) {
		found = append(found, declaredSensitiveExtent{
			locator: theWalkTheOracleAudits,
			line:    value.line,
			from:    value.column,
			through: max(value.throughLine, value.line),
			past:    value.column + columnInRunes(runeCount(value.text)),
			text:    value.text,
		})
	}
	return found
}

// TestTheContainmentOracleClaimsBytesTheWalkDoesNotAttribute is the behavioural half, and it is
// what makes the structural claim above worth making.
//
// It asserts a disagreement rather than an agreement, and asserts both sides of it through the one membership function: the oracle claims the note, and the
// walk attributes it to no value at all. Should the walk one day attribute it, this fails and says so, because a test written to separate two answers stops
// separating them the moment they converge.
func TestTheContainmentOracleClaimsBytesTheWalkDoesNotAttribute(t *testing.T) {
	document, watched := aNoteAfterTheLastBlockItem()
	text := newSource("listeners.yaml", []byte(document))

	found, parses := sensitiveExtentsOf(t, text)
	if !parses {
		t.Fatalf("the document does not parse, so the oracle has nothing to read:\n%s", document)
	}
	if !insideASensitiveExtent(text, found, watched) {
		t.Fatalf("the oracle does not claim %s, which is written inside a value the table declares "+
			"secret in full; it has drifted back to the walk's own attribution\nextents: %+v\n%s",
			watched, found, document)
	}

	// The same question, of the same function, over the walk's own attribution.
	if insideASensitiveExtent(text, walkAttributedExtents(text), watched) {
		t.Fatalf("the walk attributes %s after all, so this document no longer separates the "+
			"oracle from it; find bytes the walk drops and watch those instead", watched)
	}
}

// walkAttributedValues is what the production walk reports for a document, which this file needs in
// order to assert that the oracle does *not* agree with it. It lives here rather than in the oracle
// so that the oracle's own file stays free of the call TestTheContainmentOracleNeverAsksTheWalkItAudits
// forbids.
func walkAttributedValues(text *source) []sensitiveValue {
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		return nil
	}

	_ = normalize(text, root)
	return coalescedSensitiveValues(sensitiveValues(text, schemaLevels[levelRoot], root))
}
