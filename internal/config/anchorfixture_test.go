package config

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// AC #32 against the committed corpus rather than against documents built for the assertion.
// One accepting fixture carries every clause of it, so the anchor that proves interpolation
// happens once is the same anchor that proves precedence and completeness -- three claims about
// one document instead of three documents that could each be right alone.

// anchorCorpusRoot is the accepting fixture, normalized. It is read through the corpus
// environment the rest of testdata/valid is read through, so a reference added to it is declared
// in one place (Step 3's Implementation Note 10).
func anchorCorpusRoot(t *testing.T) ast.Node {
	t.Helper()

	document := readFixtureBytes(t, filepath.Join(validCorpus, "anchors_aliases_and_merge_keys.yaml"))
	_, root, diags := stageE(t, string(document), corpusVariables)
	if len(diags) != 0 {
		t.Fatalf("the accepting fixture reported %q", messagesOf(diags))
	}
	return root
}

// anchorCorpus is that same tree decoded, which is what every value clause reads.
func anchorCorpus(t *testing.T) map[string]any {
	t.Helper()

	return decodedDocument(t, anchorCorpusRoot(t))
}

// listenerIn is one listener of the decoded fixture, by index.
func listenerIn(t *testing.T, document map[string]any, index int) map[string]any {
	t.Helper()

	listeners, isList := document["listeners"].([]any)
	if !isList || index >= len(listeners) {
		t.Fatalf("the fixture holds %#v, want a list with a listener %d", document["listeners"], index)
	}
	return listeners[index].(map[string]any)
}

// TestTheAnchoredDestinationIsPresentInFullInEveryListenerThatAliasesIt is AC #32's first
// clause. The oracle is the fixture's own text, written out, so a destination that arrived with
// one key missing fails as sharply as one that arrived empty.
//
// It is deliberately not equality with the defining listener's destination: an alias is
// implemented by sharing the node, so the aliased and the defining destination are the same
// object and comparing them compares a pointer with itself -- a case that passes whatever the
// stage does to it. The defining listener is checked here too, for the same reason: the claim
// is that all three hold what was written.
func TestTheAnchoredDestinationIsPresentInFullInEveryListenerThatAliasesIt(t *testing.T) {
	document := anchorCorpus(t)
	written := map[string]any{
		"url":     "https://hooks.example.test/order-paid",
		"method":  "POST",
		"headers": map[string]any{"X-Escaped": "${NOT_A_REFERENCE}"},
	}

	for index := range 3 {
		received := listenerIn(t, document, index)["destination"]

		if !reflect.DeepEqual(received, written) {
			t.Errorf("listener %d has destination\n%#v\nwant the anchored mapping in full\n%#v",
				index, received, written)
		}
	}
}

// TestTheSharedRetryBlockAppearsInEveryListenerThatMergesIt is the merge half of the same
// clause: every field of the anchored block reaches every listener that merged it.
func TestTheSharedRetryBlockAppearsInEveryListenerThatMergesIt(t *testing.T) {
	document := anchorCorpus(t)
	inherited := map[string]any{
		"backoff":          "exponential",
		"initial_interval": "10s",
		"max_interval":     "1h",
		"jitter":           true,
	}

	for _, index := range []int{0, 1} {
		retry := listenerIn(t, document, index)["retry"].(map[string]any)

		for field, want := range inherited {
			if retry[field] != want {
				t.Errorf("listener %d inherited retry.%s = %v, want %v", index, field, retry[field], want)
			}
		}
	}
}

// TestAReferenceInsideAMergedMappingIsSubstitutedInEveryOrderingOfTheMerge is the third claim the
// merged anchor carries, and it is the one that needs all three mechanisms at once: the value is
// substituted once at `&shared_retry`, inherited by two listeners, and inherited past a direct key
// written above the `<<` in one of them and below it in the other.
//
// The listener that writes its direct key *above* the merge is the discriminating one. That is the
// ordering both library paths get wrong, so an expansion that lost it would drop the inherited
// entry entirely -- reference and all -- rather than deliver an unsubstituted one.
func TestAReferenceInsideAMergedMappingIsSubstitutedInEveryOrderingOfTheMerge(t *testing.T) {
	document := anchorCorpus(t)

	for _, index := range []int{0, 1} {
		retry := listenerIn(t, document, index)["retry"].(map[string]any)

		if got := retry["initial_interval"]; got != "10s" {
			t.Errorf("listener %d inherited retry.initial_interval = %v, want the substituted value", index, got)
		}
	}
}

// TestADirectlyWrittenAttemptCountSurvivesTheMergeInBothOrderings is AC #32's precedence clause
// end to end. The fixture writes max_attempts above the merge key in one listener and below it
// in the other, so a document -- not only a unit test -- covers both orderings V5 measured.
func TestADirectlyWrittenAttemptCountSurvivesTheMergeInBothOrderings(t *testing.T) {
	document := anchorCorpus(t)

	written := map[int]uint64{0: 10, 1: 20}
	for index, want := range written {
		retry := listenerIn(t, document, index)["retry"].(map[string]any)

		if retry["max_attempts"] != want {
			t.Errorf("listener %d has retry.max_attempts = %v, want the directly written %d; the merged 5 won",
				index, retry["max_attempts"], want)
		}
	}
}

// TestTheMergedFixtureLoadsCleanThroughTheEntryPoint closes SC-2's deferred value half: precedence
// is read from the resolved public tree rather than only from the normalized AST.
func TestTheMergedFixtureLoadsCleanThroughTheEntryPoint(t *testing.T) {
	path := filepath.Join(validCorpus, "anchors_aliases_and_merge_keys.yaml")

	document := readFixtureBytes(t, path)

	cfg, warnings, errs := Parse(document, filepath.Base(path), corpusEnvironment())

	if len(errs) != 0 {
		t.Fatalf("Parse returned %d diagnostics %q, want none", len(errs), messagesOf(errs))
	}
	if rendered := errs.Render(document) + warnings.Render(document); rendered != "" {
		t.Errorf("the fixture rendered\n%s\nwant nothing", rendered)
	}
	if cfg == nil {
		t.Fatal("Parse returned no resolved configuration")
	}
	for index, want := range []int{10, 20} {
		if got := cfg.Listeners[index].Delivery.Retry.MaxAttempts; got != want {
			t.Errorf("listener %d retry.max_attempts = %d, want %d", index, got, want)
		}
	}
}

// TestAReferenceInsideAnAnchoredMappingIsSubstitutedInEveryAlias is SC-6's first half: the
// substitution happens once, at the anchor, and every listener reading it sees the result.
func TestAReferenceInsideAnAnchoredMappingIsSubstitutedInEveryAlias(t *testing.T) {
	document := anchorCorpus(t)

	for index := range 3 {
		destination := listenerIn(t, document, index)["destination"].(map[string]any)

		if got := destination["url"]; got != "https://hooks.example.test/order-paid" {
			t.Errorf("listener %d has destination.url = %v, want the substituted value", index, got)
		}
	}
}

// TestAnEscapedDelimiterInsideAnAnchoredMappingIsUnescapedExactlyOnce is why stage D runs before
// stage E. The anchor is aliased twice, so an expansion that ran first -- or a stage that
// re-scanned what it had already substituted -- would unescape the same `$${` two or three times
// and hand every listener a value one dollar short.
func TestAnEscapedDelimiterInsideAnAnchoredMappingIsUnescapedExactlyOnce(t *testing.T) {
	document := anchorCorpus(t)

	for index := range 3 {
		headers := listenerIn(t, document, index)["destination"].(map[string]any)["headers"].(map[string]any)

		if got := headers["X-Escaped"]; got != "${NOT_A_REFERENCE}" {
			t.Errorf("listener %d has X-Escaped = %v, want the escape unescaped exactly once", index, got)
		}
	}
}
