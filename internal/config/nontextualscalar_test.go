package config

import "testing"

// The one enumeration of the scalars the parser reads as something other than text, seen from both
// of the questions that read it: whether a node can hold a `${...}` reference, and what tells two
// keys of one kind apart. They were two switches over one set until Step 5, and this is what keeps
// them one.

// TestBothQuestionsAboutANonTextualScalarSeeOneSet ties the two askers of nontextualscalar.go's
// enumeration together, over every kind this table can meet in a key position.
//
// They were two switches over one set until Step 5, and the drift they admitted was silent and
// one-directional: a kind the value position recognised and the identity lacked fell through to
// the *rendering*, which splits `16` and `0x10` into two keys -- so a merge admits both, the
// decode holds one key twice, and the merged entry wins over the one the author wrote. Each row
// asserts both askers answer for its kind, so a library upgrade that adds one has to be taught
// the whole enumeration rather than half of it.
func TestBothQuestionsAboutANonTextualScalarSeeOneSet(t *testing.T) {
	for kind, renderings := range nonTextualKeyRenderings {
		t.Run(kind, func(t *testing.T) {
			for _, document := range renderings {
				beneath := beneathKeyProperties(firstEntryOf(t, parsedRoot(t, document)).Key)

				if !isNonTextualScalar(beneath) {
					t.Errorf("%q is not read as a non-textual scalar, so a value position would "+
						"scan it for a ${...} reference it cannot hold", document)
				}
				if _, readAsAValue := parsedScalarValue(beneath); !readAsAValue {
					t.Errorf("%q is identified by its rendering, so a second spelling of it "+
						"survives a merge and the decode holds one key twice", document)
				}
			}
		})
	}
}

// TestTheRenderingFallbackIsReachedOnlyByAShapeHoldingNoValue is the other side of the same
// guard, asserted rather than argued. The fallback exists for the merge marker, which holds no
// value the parser reduced; if the enumeration ever stopped covering a kind, that kind would
// arrive here too and the split this fallback licenses would become reachable by an ordinary
// document.
func TestTheRenderingFallbackIsReachedOnlyByAShapeHoldingNoValue(t *testing.T) {
	marker := firstEntryOf(t, valueOfEntry(parsedRoot(t, "a: &a {p: 1}\nb:\n  <<: *a\n"), 1)).Key

	if !marker.IsMergeKey() {
		t.Fatalf("the nested mapping writes %T, want the merge marker; this would assert nothing", marker)
	}
	if _, readAsAValue := parsedScalarValue(beneathKeyProperties(marker)); readAsAValue {
		t.Error("the merge marker holds a parsed value, so nothing reaches the rendering fallback " +
			"and its cost is no longer being paid for anything")
	}
}
