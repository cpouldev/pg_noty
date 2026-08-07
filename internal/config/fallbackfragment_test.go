package config

import (
	"strings"
	"testing"
)

// This file runs the two classes the fallback branch answered wrongly because an exemption written
// about a *line* was applied to something that is not one.
//
// Both are reproductions on the branch that reads a document the parser rejected, which is the
// branch whose contract is "over-redaction is acceptable; under-redaction never is". Each was
// rendered verbatim before its repair.

// assertTheFallbackHidesEveryMarker requires a document to reach the key-scoped fallback and to
// render none of its markers. The branch is asserted rather than assumed, because a document that
// started parsing would move the claim to the other branch and pass for a reason the row is not
// about.
func assertTheFallbackHidesEveryMarker(t *testing.T, source string, markers ...string) {
	t.Helper()

	if ran, pathAware := branchTaken(plantedSecret{text: source}); pathAware {
		t.Fatalf("the document parses and reads on the %s branch, not the fallback this case is "+
			"written for:\n%s", ran, source)
	}
	if len(markers) == 0 {
		t.Fatalf("no marker is watched, so this would pass vacuously:\n%s", source)
	}

	rendered := renderEveryLineOf(source)
	for _, marker := range markers {
		if strings.Contains(rendered, marker) {
			t.Errorf("%s was rendered\nsource:\n%s\nrendered:\n%s", marker, source, rendered)
		}
	}
}

// TestAFallbackSensitiveKeySplitByACarriageReturnStillHidesItsValue is the class where the name
// itself is cut in half.
//
// A lone carriage return is a line break to the parser and not to the person who wrote the file, so
// `sec<CR>rets:` is one key nobody can see is one: neither fragment spells a sensitive name, both
// were certified readable keys by their own shape, and the value beside the second was rendered. The
// rows differ in which sensitive name is split and where the break falls, because a repair keyed on
// one spelling would leave the others.
func TestAFallbackSensitiveKeySplitByACarriageReturnStillHidesItsValue(t *testing.T) {
	for _, row := range []struct {
		name    string
		written string
	}{
		{name: "the break falls inside secrets", written: "sec\rrets: "},
		{name: "the break falls after the first rune of url", written: "u\rrl: "},
		{name: "the break falls just before the colon", written: "listen_url\r: "},
	} {
		t.Run(row.name, func(t *testing.T) {
			marker := leakSentinel + "SPLIT-KEY"
			// The trailing duplicate key is what keeps the document off the path-aware branch.
			source := "version: 1\n" + row.written + marker + "\nversion: 2\n"

			assertTheFallbackHidesEveryMarker(t, source, marker)
		})
	}
}

// TestAFragmentThatCarriesNoBytesIsStillRenderedAsWritten is the bound on the repair above. The
// fragment arm blanks whatever it is handed, so widening it until it swallows a blank line would
// pass every row above while replacing empty lines with a placeholder and costing every snippet the
// blank lines its context is laid out with.
func TestAFragmentThatCarriesNoBytesIsStillRenderedAsWritten(t *testing.T) {
	const survives = "name: order_paid"
	source := "version: 1\n" + survives + "\r\n\nversion: 2\n"

	rendered := renderEveryLineOf(source)
	if strings.Contains(rendered, redactionPlaceholder) {
		t.Errorf("a fragment carrying no bytes was replaced; blanking one protects nothing and "+
			"costs a snippet the blank line it is laid out with:\n%s", rendered)
	}
}

// TestAnUnclosedQuoteBelowASensitiveKeyKeepsCoveringTheLinesAfterIt is the class where the state a
// blanked line leaves open was thrown away.
//
// The line opening the quote is blanked, so it is not the leak; what leaked is the line after it,
// which the parser itself reports as still inside that scalar. Handing on "nothing is open" let the
// next line be certified a readable key of its own and rendered as written.
func TestAnUnclosedQuoteBelowASensitiveKeyKeepsCoveringTheLinesAfterIt(t *testing.T) {
	for _, row := range []struct {
		name   string
		opener string
	}{
		{name: "a double quote", opener: `"unclosed`},
		{name: "a single quote", opener: "'unclosed"},
		{name: "a flow mapping", opener: "{unclosed"},
	} {
		t.Run(row.name, func(t *testing.T) {
			marker := leakSentinel + "AFTER-OPENER"
			source := "secrets:\n" + row.opener + "\nother: " + marker + "\n"

			assertTheFallbackHidesEveryMarker(t, source, marker)
		})
	}
}
