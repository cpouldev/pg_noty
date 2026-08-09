package config

import (
	"reflect"
	"testing"
)

// What a `<<` may point at: the shapes it resolves to a mapping, and the ones it refuses. What
// the expansion then does with those mappings is mergekey_test.go's subject, and what makes two
// keys one key is keyidentity_test.go's.

// TestANonMappingMergeSourceYieldsExactlyOnePositionedDiagnostic is stage E's second error
// condition, and the position is the decision worth asserting: the diagnostic points at the
// anchor's value -- the line the author has to change -- rather than at the `<<` that named it.
func TestANonMappingMergeSourceYieldsExactlyOnePositionedDiagnostic(t *testing.T) {
	// `columns: &shared ` is seventeen runes, so the anchored sequence begins at rune 18.
	const document = "columns: &shared [id, total]\nretry:\n  <<: *shared\n"

	_, _, diags := stageE(t, document, nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want exactly one", len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], nonMappingMergeSource, 1, 18)
}

// TestAnAnchorAMergeSourceDeclaresIsAvailableToTheAliasesAfterIt is the consequence of resolving
// a source where the walk meets it rather than after the mapping has been read: `<<: &m {...}`
// declares an anchor, and a document aliasing it further down is legal YAML.
//
// A stage that refused the anchored source would also never record the name, so one authoring
// spelling would cost the author a diagnostic per alias as well as the one about the source.
func TestAnAnchorAMergeSourceDeclaresIsAvailableToTheAliasesAfterIt(t *testing.T) {
	decoded := decodedDocument(t, normalized(t, "use:\n  <<: &m {inherited: merged}\nother: *m\n"))

	if got := decoded["other"]; !reflect.DeepEqual(got, map[string]any{"inherited": "merged"}) {
		t.Errorf("other = %#v, want the mapping the merge source anchored", got)
	}
}

// TestAMergeSourceNamingAnAnchorWrittenBelowItIsRefused holds the rule the anchor table's shape
// already states for every other position: an anchor is available to what follows it, not to what
// precedes it (normalize.go's `anchors` field).
//
// The anchor is written in the same mapping as the `<<` that names it, which is the one placement
// that discriminates: a stage resolving its sources after reading the whole mapping would have
// recorded `&later` by then and would accept a document no other position in this stage accepts.
// Resolving each source where the walk meets it is what keeps the one rule everywhere.
func TestAMergeSourceNamingAnAnchorWrittenBelowItIsRefused(t *testing.T) {
	// `  <<: ` is six runes, so the alias begins at rune 7 of line 2.
	_, _, diags := stageE(t, "use:\n  <<: *later\n  later: &later {a: 1}\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing a forward reference", len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], undefinedAlias, 2, 7)
}

// TestAMergeSourceNamingAnAnchorWrittenAboveItInTheSameMappingIsAccepted is the other side of that
// guard (.claude/rules/test-both-sides-of-an-exclusion-guard.md): the rule is about order, not
// about the anchor and the `<<` sharing a mapping, so the legal placement must still merge.
func TestAMergeSourceNamingAnAnchorWrittenAboveItInTheSameMappingIsAccepted(t *testing.T) {
	used := decodedDocument(t, normalized(t, "use:\n  base: &b {inherited: merged}\n  <<: *b\n"))["use"].(map[string]any)

	if used["inherited"] != "merged" {
		t.Errorf("use = %#v, want the key the anchor above the merge key holds", used)
	}
}

// TestASourceThatIsNoMappingIsRefusedOncePerOffendingToken carries the case above across the
// wrapper spellings it does not write, and adds the count to the claim: whatever a source is
// written inside, exactly one diagnostic reaches the author and it is the one that says what is
// wrong. Two arms judging the same node is how a token collects a second, contradicting message.
//
// Every position is the value the wrapper carries rather than the wrapper's own introducer -- the
// line the author has to change -- which is the same choice that puts two listeners merging one
// broken anchor on a single position. A list that is not a list of mappings is refused as one
// value for the same reason: `columns: [id, total]` aliased into a `<<` is one mistake about the
// list, not one about each column name.
func TestASourceThatIsNoMappingIsRefusedOncePerOffendingToken(t *testing.T) {
	sources := map[string]struct {
		document    string
		line, colum int
	}{
		// `count: &shared ` is fifteen runes, so the anchored scalar begins at rune 16.
		"an alias to a scalar": {document: "count: &shared 5\nretry:\n  <<: *shared\n", line: 1, colum: 16},
		// `  <<: ` is six runes, so the list written in place begins at rune 7 of line 2.
		"a list holding a scalar": {document: "retry:\n  <<: [1]\n", line: 2, colum: 7},
		// `  <<: &s ` is nine runes, so the anchored list begins at rune 10 of line 2.
		"an anchored list holding a scalar": {document: "retry:\n  <<: &s [1]\n", line: 2, colum: 10},
		// `  <<: !!seq ` is twelve runes, so the tagged list begins at rune 13 of line 2 -- and is
		// reported at 12, the space before it, because the parser gives a tagged value's own token
		// one column less than the rune it starts at
		// (TestGoccyReportsATaggedValueOneColumnBeforeItBegins). The caret is a column early for
		// this one spelling; the diagnostic is still one, and still about the list.
		"a tagged list holding a scalar": {document: "retry:\n  <<: !!seq [1]\n", line: 2, colum: 12},
		// `  <<: [` is seven runes and `{p: 1}, ` eight more, so a list whose second element is
		// unmergeable is still refused at the list, at rune 7.
		"a list holding one mapping and one scalar": {document: "retry:\n  <<: [{p: 1}, 2]\n", line: 2, colum: 7},
	}

	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			_, _, diags := stageE(t, source.document, nil)

			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics %q, want exactly one for the one offending token",
					len(diags), messagesOf(diags))
			}
			assertDiagnostic(t, diags[0], nonMappingMergeSource, source.line, source.colum)
		})
	}
}

// TestAnUndefinedAliasInAMergeListIsRefusedOnlyOnce is where the two refusals of this position
// meet, and the count is the claim. The list holds one mapping and one name nothing declared: the
// name is refused where it was resolved, and the list must not then be refused a second time for
// holding what is left of it.
func TestAnUndefinedAliasInAMergeListIsRefusedOnlyOnce(t *testing.T) {
	// Line 2 is `  <<: [*a, *nowhere]`; `  <<: [*a, ` is eleven runes, so the alias begins at 12.
	_, _, diags := stageE(t, "a: &a {p: 1}\nuse:\n  <<: [*a, *nowhere]\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one about the name that found nothing",
			len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], undefinedAlias, 3, 12)
}

// TestASourceWrittenInPlaceIsNormalizedBeforeItIsMerged is the reason a merge entry's value is
// not simply skipped. The walk leaves a merge entry alone so that an alias in it survives long
// enough to be told apart from an anchor holding no mapping -- and a source written out in place
// would then be copied in with its own aliases and its own merge key unresolved.
func TestASourceWrittenInPlaceIsNormalizedBeforeItIsMerged(t *testing.T) {
	documents := map[string]struct {
		document string
		key      string
		want     any
	}{
		"an alias inside the source": {
			document: "shared: &shared https://example.test\nuse:\n  <<: {url: *shared}\n",
			key:      "url",
			want:     "https://example.test",
		},
		"a merge key inside the source": {
			document: "base: &base {deep: reached}\nuse:\n  <<: {<<: *base}\n",
			key:      "deep",
			want:     "reached",
		},
	}

	for name, tc := range documents {
		t.Run(name, func(t *testing.T) {
			used := decodedDocument(t, normalized(t, tc.document))["use"].(map[string]any)

			if used[tc.key] != tc.want {
				t.Errorf("use.%s = %v, want %v; the source reached the target unreduced", tc.key, used[tc.key], tc.want)
			}
		})
	}
}
