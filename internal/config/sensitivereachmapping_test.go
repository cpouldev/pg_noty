package config

import (
	"strings"
	"testing"
)

// This file is the reach half of the block-mapping question.
//
// A round earlier, writtenStartOf learned that a block mapping is the one shape whose own token sits
// inside its text, so redaction begins at the mapping's first line rather than at its colon. Nothing
// revisited how far such a value *reaches*, though both are the same question about the same shape:
// the walk that blanks the lines past the anchor takes its membership rule from the anchor line's
// indentation, and for a block mapping the anchor line is the value's own first entry -- so every
// sibling entry sits at exactly that indentation and the walk stopped at the second one.

// TestASensitiveBlockMappingIsRedactedThroughItsLastEntry plants the strongest declaration in the
// table -- signing.secrets, whose whole value goes -- written in the one shape whose sibling entries
// share the anchor line's column.
func TestASensitiveBlockMappingIsRedactedThroughItsLastEntry(t *testing.T) {
	source := "version: 1\n" + signingSecrets(
		"      secrets:\n"+
			"        first: "+leakSentinel+"ENTRY-1\n"+
			"        second: "+leakSentinel+"ENTRY-2\n"+
			"        third: "+leakSentinel+"ENTRY-3\n")

	assertEveryMarkerIsRedacted(t, source, leakSentinel+"ENTRY-1",
		leakSentinel+"ENTRY-2", leakSentinel+"ENTRY-3")
}

// TestAUrlPasswordBlockMappingIsRedactedThroughItsLastEntry is the same shape under the other
// declaration, because the two arms reach blankValueReach by different routes: entireValue through
// redactValue's prefix test, urlPassword through the connection-string grammar.
func TestAUrlPasswordBlockMappingIsRedactedThroughItsLastEntry(t *testing.T) {
	source := "version: 1\ndatabase:\n" +
		"  url:\n" +
		"    host: " + leakSentinel + "HOST\n" +
		"    port: " + leakSentinel + "PORT\n" +
		"    password: " + leakSentinel + "PASSWORD\n"

	assertEveryMarkerIsRedacted(t, source, leakSentinel+"HOST",
		leakSentinel+"PORT", leakSentinel+"PASSWORD")
}

// TestABlockMappingBeneathAnUndeclaredParentIsRedactedThroughItsLastEntry reaches the same walk
// from the arm that fails closed on an unreadable key, where the whole value is taken because no
// declaration can be looked up for it.
func TestABlockMappingBeneathAnUndeclaredParentIsRedactedThroughItsLastEntry(t *testing.T) {
	source := "version: 1\ntypo:\n" +
		"  url:\n" +
		"    host: " + leakSentinel + "UNKNOWN-1\n" +
		"    password: " + leakSentinel + "UNKNOWN-2\n"

	assertEveryMarkerIsRedacted(t, source, leakSentinel+"UNKNOWN-1", leakSentinel+"UNKNOWN-2")
}

// TestASensitiveBlockMappingDoesNotReachItsParentsNextKey bounds the reach on the other side. A walk
// widened until it swallows the dedent would pass every test above while blanking the rest of the
// document, and over-redaction that far costs a diagnostic the context it exists to show.
func TestASensitiveBlockMappingDoesNotReachItsParentsNextKey(t *testing.T) {
	const survives = "algorithm: hmac-sha256"
	source := "version: 1\n" + signingSecrets(
		"      secrets:\n"+
			"        first: "+leakSentinel+"BOUNDED-1\n"+
			"        second: "+leakSentinel+"BOUNDED-2\n"+
			"      "+survives+"\n")

	rendered := renderEveryLineOf(source)
	if strings.Contains(rendered, leakSentinel) {
		t.Fatalf("a marker survived, so the bound below would be measured on the wrong reach:\n%s",
			rendered)
	}
	if !strings.Contains(rendered, survives) {
		t.Errorf("the sibling key %q written at the sensitive key's own indentation was blanked "+
			"with the value, so the reach ran past the dedent that ends it:\n%s", survives, rendered)
	}
}

// assertEveryMarkerIsRedacted renders a diagnostic on every line of a document and reports each
// marker that survived. Every marker is asserted separately rather than as a set, so a repair that
// reaches the second entry and stops fails naming the third.
func assertEveryMarkerIsRedacted(t *testing.T, source string, markers ...string) {
	t.Helper()

	if len(markers) == 0 {
		t.Fatalf("no marker to search for, so the claim below is satisfied by asking nothing:\n%s",
			source)
	}
	if _, pathAware := branchTaken(plantedSecret{text: source}); !pathAware {
		t.Fatalf("the document does not parse, so it reads on the fallback rather than on the "+
			"path-aware branch this case is written for:\n%s", source)
	}

	rendered := renderEveryLineOf(source)
	// An absence found in nothing is not containment: the same search passes when the renderer
	// produced no output at all, which is the shape assertNoMarkerSurvives guards against too.
	if !strings.Contains(rendered, "listeners.yaml:") {
		t.Fatalf("nothing was rendered, so the search below would pass vacuously:\n%s", source)
	}
	for _, marker := range markers {
		if strings.Contains(rendered, marker) {
			t.Errorf("%s was rendered\nsource:\n%s\nrendered:\n%s", marker, source, rendered)
		}
	}
}

// The class this file's reproduction belonged to -- author-written runes inside a container the
// table declares secret in full -- and the bound on it are in containerinterior_test.go, because
// the repair they judge moved from "the container closes on a later line" to "the container holds
// runes no child claims" and the two are different subjects. The corpus entry that minted the
// reproduction is testdata/fuzz/FuzzRenderedTextNeverQuotesASecret/8f561b44265cd89e, and it is run
// by TestASensitiveContainerHoldingRunesNoChildClaimsIsBlankedWholesale's first row.
