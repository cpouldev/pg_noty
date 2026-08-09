package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// The two fields a sequence holds its elements in, and the measurement that says they hold the
// same nodes.
//
// It is a file of its own because two stages rest on it and neither owns it. Stage D substitutes
// through Values and needs Entries to see the result; stage E writes a resolved alias through
// both and refuses a sequence whose fields disagree, which is a refusal whose whole premise is
// this pin (unreducibleshape.go's elementFieldsAgree).

// TestGoccyGivesASequenceTheSameNodesInValuesAndEntries pins the library behaviour both
// recursions depend on: the elements reached through Values are the very nodes reached through
// Entries, so a replacement written through one is seen through the other.
//
// Skill Pattern 9's own example iterates a field named Elements, which this version does not
// have (Step 3's Implementation Note 3); a version that made Values a copy would silently
// discard every substitution into a list, which is where signing secrets are written.
//
// Both spellings of a sequence are measured, and the flow one is not decoration: a pin covering
// only the block spelling would leave the stage-E refusal's premise unmeasured for the spelling
// that reaches it. The empty row is the boundary at which "both hold the same nodes" could be
// satisfied by one of them being nil, so each row states the length it expects rather than
// comparing the two fields alone.
func TestGoccyGivesASequenceTheSameNodesInValuesAndEntries(t *testing.T) {
	sequences := map[string]struct {
		document string
		length   int
	}{
		"a block sequence":       {document: "l:\n  - one\n  - two\n", length: 2},
		"a flow sequence":        {document: "l: [one, two]\n", length: 2},
		"an empty flow sequence": {document: "l: []\n", length: 0},
	}

	for name, written := range sequences {
		t.Run(name, func(t *testing.T) {
			sequence := valueOfFirstEntry(parsedRoot(t, written.document)).(*ast.SequenceNode)

			if len(sequence.Values) != written.length || len(sequence.Entries) != written.length {
				t.Fatalf("Values holds %d nodes and Entries %d, want %d each",
					len(sequence.Values), len(sequence.Entries), written.length)
			}
			for i := range sequence.Values {
				if sequence.Values[i] != sequence.Entries[i].Value {
					t.Errorf("Values[%d] is not Entries[%d].Value; a replacement through one would be lost", i, i)
				}
			}
		})
	}
}
