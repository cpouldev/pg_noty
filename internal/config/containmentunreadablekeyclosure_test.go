package config

import (
	"strings"
	"testing"
)

// This file closes the unreadable-key dimension of the containment grid.
//
// It is separate from containmentclosure_test.go because the authority differs, and the difference
// is the whole point: the dimensions there are closed over production enumerations that *are* the
// contract, while this one is closed over YAML's grammar, because the constant it judges is a
// hypothesis about that grammar rather than a statement of it.

// TestEveryUnreadableKeyIndicatorHasALayout closes the unreadable-key family, in both directions,
// against the spellings YAML permits rather than against the constant the production matcher tests.
//
// The size of this dimension used to be `len(unreadableKeyIndicators)` -- the symbol under test. A
// key introducer YAML permits and that constant omits was therefore unreportable here, because the
// generator's size was defined by the thing whose omission it exists to find; the count matched, and
// a missing indicator would have kept it matching by also being a missing spelling.
//
// The spellings are the authority now. Each names a construct YAML 1.2 introduces before a key --
// an alias (section 7.1), an anchor property and a tag property (section 6.9), and the explicit-key
// indicator (section 7.4.2) -- and the constant is required to hold the byte each opens with. The
// other direction stays, because an indicator the constant holds and no spelling writes is an arm
// the generator never plants.
func TestEveryUnreadableKeyIndicatorHasALayout(t *testing.T) {
	for _, spelling := range unreadableKeySpellings {
		if spelling.prefix == "" {
			t.Errorf("the %s spelling writes no prefix, so it opens with no indicator at all",
				spelling.name)
			continue
		}
		if !strings.ContainsRune(unreadableKeyIndicators, rune(spelling.prefix[0])) {
			t.Errorf("the %s spelling opens with %q, which %q does not hold, so a key written that "+
				"way is read as one the fallback can decode and its value is rendered",
				spelling.name, string(spelling.prefix[0]), unreadableKeyIndicators)
		}
	}
	for _, indicator := range []byte(unreadableKeyIndicators) {
		if !anyKeySpellingOpensWith(indicator) {
			t.Errorf("no unreadable-key spelling opens with %q, so the arm it introduces is a branch "+
				"no containment run can reach", string(indicator))
		}
	}
	if len(unreadableKeyLayouts()) != len(unreadableKeySpellings) {
		t.Errorf("%d layouts for %d unreadable-key spellings", len(unreadableKeyLayouts()),
			len(unreadableKeySpellings))
	}
}

func anyKeySpellingOpensWith(indicator byte) bool {
	for _, spelling := range unreadableKeySpellings {
		if spelling.prefix != "" && spelling.prefix[0] == indicator {
			return true
		}
	}
	return false
}
