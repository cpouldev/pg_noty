package config

import (
	"strings"
	"testing"
)

// This file pins the order governanceOf asks its two questions in.
//
// The order is real behaviour and was described rather than asserted: the comment at
// sensitivekeyforms.go named a test that existed nowhere in the package, and swapping the two arms
// left the whole suite green.
//
// A precedence is only behaviour once an input violates both rules at once. Every line that writes
// one spelling is answered identically by either order, so the case below writes both -- the
// explicit-key value indicator that opens a key written on an earlier line, and a sensitive name
// with its own colon after it.

// aLineWritingBothSpellings opens an explicit key's value *and* writes a sensitive name with its own
// colon later on the same line. Read as the explicit-key form it is the value half of a key written
// elsewhere, so the whole of this line is that key's; read as an inline key its value begins after
// `url:`, so everything before that survives. What is written *between* the two is exactly what the
// readings disagree about, which is where a case has to put its marker.
func aLineWritingBothSpellings(between string) string {
	return ": " + between + " url: value"
}

// TestALineWritingBothSpellingsIsGovernedByTheReadingThatHidesMore is the precedence itself,
// asserted on the verdict the fallback loop acts on.
func TestALineWritingBothSpellingsIsGovernedByTheReadingThatHidesMore(t *testing.T) {
	line := aLineWritingBothSpellings("between")
	content, _ := pastIndentation(line)

	at, governs := governanceOf(line, content)
	if governs != thisLineAndTheBlockBelow {
		t.Fatalf("governanceOf(%q) = %v, want thisLineAndTheBlockBelow; this line writes both "+
			"spellings and the explicit-key reading is the one that hides more", line, governs)
	}

	// The offsets the two readings answer with differ, which is what makes the verdict above load
	// bearing rather than a label: the explicit-key reading governs from the line's first content
	// rune, the inline one only from past `url:`.
	if want := byteOffset(0); at != want {
		t.Errorf("the governed bytes begin at offset %d, want %d -- the whole of this line's "+
			"content, not the part after a colon inside it", at, want)
	}
	inline, found := earliestSensitiveKeyEnd(line)
	if !found {
		t.Fatal("no inline sensitive spelling matches this line, so it violates only one rule and " +
			"either order answers it the same way")
	}
	if inline <= at {
		t.Errorf("the inline reading governs from offset %d, which is not past the explicit-key "+
			"reading's %d; the two readings no longer differ on this line, so it cannot separate "+
			"the orders", inline, at)
	}
}

// TestTheReversedPrecedenceWouldRenderTheBytesBeforeTheInlineKey is the consequence, stated as the
// document a user would see. A verdict is a label; what the order actually decides is how much of
// the line survives, and the reversed order leaves everything before `url:` rendered.
func TestTheReversedPrecedenceWouldRenderTheBytesBeforeTheInlineKey(t *testing.T) {
	marker := leakSentinel + "BEFORE-THE-INLINE-KEY"
	// The explicit key's name is written on the line above, as YAML 1.2 section 7.4.2 permits. The
	// marker sits on its value line, before the inline sensitive name, so only the explicit-key
	// reading takes those bytes. The unclosed flow sequence is what keeps the document off the
	// path-aware branch.
	source := "? a key\n" + aLineWritingBothSpellings(marker) + "\nlisteners: [one, two\n"

	if ran, pathAware := branchTaken(plantedSecret{text: source}); pathAware {
		t.Fatalf("the document parses and reads on the %s branch, not the fallback whose "+
			"precedence this is:\n%s", ran, source)
	}

	rendered := renderEveryLineOf(source)
	if strings.Contains(rendered, marker) {
		t.Errorf("%s was rendered; the inline reading governs only what follows `url:`, so the "+
			"order that answers first decides whether these bytes survive\nsource:\n%s\nrendered:\n%s",
			marker, source, rendered)
	}
}
