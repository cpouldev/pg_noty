package config

import "testing"

// Where an anchor can be written on a key, and that the walk records every one of them. It is a
// question about one position rather than about the walk, so it is asked here rather than beside
// the walk that answers it (normalize_test.go).

// anchoredKeySpellings is every arrangement of node properties an anchor can be written inside
// in a key position. YAML permits an anchor and a tag in either order, and permits either
// arrangement inside an explicit `?` key, so the wrappers the walk has to pass through to reach
// the anchor are the wrappers keyTextOf already reads past to reach the text.
//
// Every row declares `k` on a key and then aliases it, so a row whose wrapper the walk does not
// pass through fails as the spurious "not defined earlier in this document" it would produce on
// a legal document. Measured before the arm that reads them was written: the two rows with a tag
// outside the anchor failed exactly that way.
var anchoredKeySpellings = map[string]string{
	"an anchor on a bare key":                "&k key: v\nuse: *k\n",
	"an anchor inside an explicit key":       "? &k key\n: v\nuse: *k\n",
	"a tag written outside the anchor":       "!!str &k key: v\nuse: *k\n",
	"a tag written inside the anchor":        "&k !!str key: v\nuse: *k\n",
	"a tagged anchor inside an explicit key": "? !!str &k key\n: v\nuse: *k\n",
	"an anchored tag inside an explicit key": "? &k !!str key\n: v\nuse: *k\n",
}

// TestAnAnchorWrittenOnAKeyIsAvailableToLaterAliases covers the one place an anchor can be
// defined that is not a value position. The stage rewrites no key -- keys are stage D's, and an
// alias in one is refused there -- but it must still record what a key-position anchor names, or
// a legal document would be told its alias refers to nothing.
//
// It is asserted over the whole spelling set rather than over the one arrangement that first
// occurred to a reader: the walk that records these enumerates wrappers, and an enumeration is
// only as closed as the set that tests it.
func TestAnAnchorWrittenOnAKeyIsAvailableToLaterAliases(t *testing.T) {
	for name, document := range anchoredKeySpellings {
		t.Run(name, func(t *testing.T) {
			root := normalized(t, document)

			if got := textIn(t, root, "$.use"); got != "key" {
				t.Errorf("use decoded to %q, want the text the anchored key holds", got)
			}
		})
	}
}
