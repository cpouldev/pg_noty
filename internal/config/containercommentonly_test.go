package config

import (
	"strings"
	"testing"
)

// This file is the null-value corner of containerinterior.go's class: a value the table declares
// secret in full which the parser attributes no node to, because everything written inside it is a
// comment. It is separate from the container rows because the subject differs -- there the runes sit
// beside children, here there are no children at all -- and because the repair each judges is in a
// different function.

// TestASensitiveKeyWhoseWrittenContentIsOnlyACommentStillLosesIt runs the class the re-anchored
// containment oracle found: a value the table declares secret in full whose whole written content
// is a comment, which the parser attributes to no node at all.
//
// redactValue used to answer "a null node: nothing was written to hide" and return, which is true
// of the node and false of the document -- the comment beneath the key is inside the value, and it
// reached rendered output. Both spellings are run, because a comment can be the whole of a block
// mapping's content or the whole of a block sequence item, and the two arrive by different routes.
//
// The second row is the bound: where the value really is absent, the reach must blank nothing, or
// the repair would take the sibling key that follows and cost every diagnostic beneath a written-out
// key its context.
func TestASensitiveKeyWhoseWrittenContentIsOnlyACommentStillLosesIt(t *testing.T) {
	const survives = "algorithm: hmac-sha256"

	for _, row := range []struct {
		name    string
		written string
	}{
		{
			name:    "the whole of the value is a comment",
			written: "      secrets:\n        # " + leakSentinel + "ONLY-CONTENT\n",
		},
		{
			name: "the whole of the only sequence item is a comment",
			written: "      secrets:\n        - # " + leakSentinel + "ITEM-1\n" +
				"        - # " + leakSentinel + "ITEM-2\n",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			source := "version: 1\n" + signingSecrets(row.written+"      "+survives+"\n")

			markers := markersIn(source)
			if len(markers) == 0 {
				t.Fatalf("the row plants no marker, so this would pass vacuously:\n%s", source)
			}
			assertEveryMarkerIsRedacted(t, source, markers...)

			if rendered := renderEveryLineOf(source); !strings.Contains(rendered, survives) {
				t.Errorf("the sibling key %q written at the sensitive key's own indentation was "+
					"blanked, so the reach ran past the value:\n%s", survives, rendered)
			}
		})
	}
}

// TestASensitiveKeyWrittenWithNoValueAtAllBlanksNothing is the other half of the bound above, and it
// is separate because the row shape differs: there is no marker to require absent, only a document
// whose every line must survive.
func TestASensitiveKeyWrittenWithNoValueAtAllBlanksNothing(t *testing.T) {
	const survives = "algorithm: hmac-sha256"
	source := "version: 1\n" + signingSecrets("      secrets:\n      "+survives+"\n")

	if rendered := renderEveryLineOf(source); !strings.Contains(rendered, survives) {
		t.Errorf("a key written with no value at all blanked the sibling below it; a reach that "+
			"takes a line the value does not occupy costs every diagnostic there its context:\n%s",
			rendered)
	}
}
