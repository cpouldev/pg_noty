package config

import (
	"slices"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// Merge-key expansion: what `<<` contributes, what it may never override, and how it is
// recognised. The library's own answers to the same three questions are pinned next door in
// goccymerge_test.go, and this file is what the loader does instead.

// mergeKeyText is `<<` as an author writes it. It is spelled here, in a test, because the
// expansion must never compare a key to it -- TestNoProductionFileComparesAKeyToTheMergeMarker
// is what keeps that true.
const mergeKeyText = "<<"

// The document V5 was measured on, in the two orderings YAML gives the same answer for. Only the
// position of the direct key differs, which is what makes the pair discriminating: an expansion
// that took the last writer passes one and fails the other.
//
// goccymerge_test.go's pins read these same two constants. A pin exists to measure the library on
// the document the product is asserted against, so the two must be one declaration: a second copy
// under other names detaches silently the first time either is edited.
const (
	directKeyBelowTheMergeKey = "base: &b\n  max_attempts: 5\n  backoff: exponential\n" +
		"retry:\n  <<: *b\n  max_attempts: 10\n"
	directKeyAboveTheMergeKey = "base: &b\n  max_attempts: 5\n  backoff: exponential\n" +
		"retry:\n  max_attempts: 10\n  <<: *b\n"
)

// mergedRetryValues is what both orderings must produce: the directly written attempt count, and the
// inherited backoff the author did not write.
var mergedRetryValues = map[string]any{"max_attempts": uint64(10), "backoff": "exponential"}

// assertDirectKeysSurviveTheMerge is the precedence invariant as an assertion: at path, every key
// the author wrote directly holds what they wrote, every key they did not write holds what a
// source brought in, and the `<<` itself is gone.
//
// The two ordering cases and the invariant case all call it, so the invariant they instantiate is
// a compile-time dependency between them rather than a doc comment: it cannot be deleted while
// any of the three still stands.
func assertDirectKeysSurviveTheMerge(t *testing.T, document, path string, want map[string]any) {
	t.Helper()

	merged, isMapping := decodedDocument(t, normalized(t, document))[path].(map[string]any)
	if !isMapping {
		t.Fatalf("%s did not decode to a mapping", path)
	}

	for key, expected := range want {
		if merged[key] != expected {
			t.Errorf("%s.%s = %v, want %v", path, key, merged[key], expected)
		}
	}
	if _, kept := merged[mergeKeyText]; kept {
		t.Errorf("the merge key itself survived into the decoded %s", path)
	}
}

// TestADirectKeyWrittenBelowTheMergeKeyWins is V5's ordering A. It is one instance of
// TestEveryDirectKeyIsPresentBeforeAnyMergedOneIsConsidered, which is the invariant that makes
// both orderings the same case: a reader reintroducing order dependence has to fail that test,
// not merely pick the ordering this one happens to write.
func TestADirectKeyWrittenBelowTheMergeKeyWins(t *testing.T) {
	assertDirectKeysSurviveTheMerge(t, directKeyBelowTheMergeKey, "retry", mergedRetryValues)
}

// TestADirectKeyWrittenAboveTheMergeKeyWins is V5's ordering B, the one both library paths get
// wrong: native decode fails outright and AllowDuplicateMapKey() hands the merged 5 back. It is
// the other instance of TestEveryDirectKeyIsPresentBeforeAnyMergedOneIsConsidered's invariant.
func TestADirectKeyWrittenAboveTheMergeKeyWins(t *testing.T) {
	assertDirectKeysSurviveTheMerge(t, directKeyAboveTheMergeKey, "retry", mergedRetryValues)
}

// TestEveryDirectKeyIsPresentBeforeAnyMergedOneIsConsidered is the invariant the two orderings
// above are two instances of, asserted in one document so that an order-dependent expansion has
// nowhere to hide: one direct key is written above the merge and one below it, and both must
// survive while the key neither of them names is inherited.
func TestEveryDirectKeyIsPresentBeforeAnyMergedOneIsConsidered(t *testing.T) {
	const document = "base: &b\n  first: inherited\n  second: inherited\n  third: inherited\n" +
		"use:\n  first: written\n  <<: *b\n  second: written\n"

	assertDirectKeysSurviveTheMerge(t, document, "use",
		map[string]any{"first": "written", "second": "written", "third": "inherited"})
}

// TestADirectKeyWinsAgainstAMergedKeyWrittenInAnotherRendering is the precedence invariant over
// the dimension the wrapper spellings cannot reach. `16` and `0x10` are one key, so the direct
// `16` must beat the merged `0x10` exactly as it beats a merged `16`.
//
// Measured before keyIdentity read the parsed value: the expansion admitted the merged entry as a
// second key, and the decode then collapsed the two spellings and kept the *merged* one -- the
// direct key silently losing, which is the failure V5 is pinned for, reached through this stage's
// own output rather than through a library option.
func TestADirectKeyWinsAgainstAMergedKeyWrittenInAnotherRendering(t *testing.T) {
	const document = "base: &b\n  0x10: from_the_merge\nuse:\n  16: written\n  <<: *b\n"

	assertDirectKeysSurviveTheMerge(t, document, "use", map[string]any{"16": "written"})
}

// TestTheEarlierSourceOfAMultiMergeWins covers `<<: [*a, *b]`, where YAML's own rule is that the
// earlier source of the list is the one that supplies a key both hold.
func TestTheEarlierSourceOfAMultiMergeWins(t *testing.T) {
	const document = "a: &a {shared: from_a, only_a: yes}\nb: &b {shared: from_b, only_b: yes}\n" +
		"use:\n  <<: [*a, *b]\n"

	used := decodedDocument(t, normalized(t, document))["use"].(map[string]any)

	if used["shared"] != "from_a" {
		t.Errorf("use.shared = %v, want the first source's value", used["shared"])
	}
	if used["only_a"] == nil || used["only_b"] == nil {
		t.Errorf("use = %#v, want a key from each source", used)
	}
}

// TestTheExpansionWritesNothingIntoTheEntriesItWasGiven is expandedWith's purity, asserted rather
// than commented. Its result is a mapping's new entry slice, and the direct entries it is given
// are the slice the walk has been collecting into -- which has spare capacity, so appending onto
// it would write inherited entries into the caller's own array through an argument the signature
// says is only read.
func TestTheExpansionWritesNothingIntoTheEntriesItWasGiven(t *testing.T) {
	source := parsedRoot(t, "a: 1\nb: 2\n").(*ast.MappingNode)

	direct := make([]*ast.MappingValueNode, 0, 4)
	direct = append(direct, source.Values[0])
	spare := direct[:cap(direct)]

	expandedWith(direct, []*ast.MappingNode{source})

	for i, entry := range spare[len(direct):] {
		if entry != nil {
			t.Errorf("the expansion wrote %q into the caller's spare capacity at %d",
				entry.Key.String(), len(direct)+i)
		}
	}
}

// TestTheMergeEntryIsGoneFromTheNormalizedMapping is the fourth step of skill Pattern 4: `<<`
// is YAML syntax rather than a schema field, so the structural pass must never meet one.
func TestTheMergeEntryIsGoneFromTheNormalizedMapping(t *testing.T) {
	root := normalized(t, directKeyBelowTheMergeKey)

	for _, entry := range nodeIn(t, root, "$.retry").(*ast.MappingNode).Values {
		if entry.Key.IsMergeKey() {
			t.Error("a merge entry survived normalization, so stage F would have to skip one")
		}
	}
	if slices.Contains(declaredKeyNamesOf(t, root, "$.retry"), mergeKeyText) {
		t.Error("the normalized mapping still holds a key spelled as the merge marker")
	}
}

// TestAMergeIntroducesNoKeyStageDHasNotAlreadyChecked pins the argument that lets the expansion
// re-check only the keys it invents.
//
// A merge source is a mapping of this same document, so its keys were checked where they were
// written -- and a document whose anchor holds a key with a reference in it never reaches stage
// E at all. The claim is that stage D stops it, so it is asserted rather than argued.
func TestAMergeIntroducesNoKeyStageDHasNotAlreadyChecked(t *testing.T) {
	const document = "base: &b\n  ${HEADER}: value\nuse:\n  <<: *b\n"

	_, _, errs := Parse([]byte(document), interpolationFixture, MapEnv(map[string]string{"HEADER": "X-Tenant"}))

	if len(errs) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one stage D raises about the anchored key",
			len(errs), messagesOf(errs))
	}
	if errs[0].Rule != RuleInterpolate || errs[0].Msg != keyReferenceMessage {
		t.Errorf("the run reported %q from %q, want stage D's key refusal", errs[0].Msg, errs[0].Rule)
	}
}

// declaredKeyNamesOf is the key text of every entry of the mapping at path.
func declaredKeyNamesOf(t *testing.T, root ast.Node, path string) []string {
	t.Helper()

	var names []string
	for _, entry := range nodeIn(t, root, path).(*ast.MappingNode).Values {
		text, _ := keyTextOf(entry.Key)
		names = append(names, text)
	}
	return names
}
