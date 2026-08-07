package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// The measurement behind R42's completeness: what the parser's own duplicate detection catches at
// stage B, what it misses, and what the miss costs if nothing else catches it. The refusals
// themselves are headercollision_test.go's.
//
// It is a pin file rather than part of the refusal's own tests, following sequencefields_test.go's
// precedent: a claim about the library belongs where an upgrade that changes it fails by name.

// twoRenderingsOfOneHeaderName writes the integer one twice, as `1` and as `0x1`. It is a header
// mapping deliberately: every name is legal there, so nothing else in the pipeline has a word to
// say about the document and the duplicate would otherwise reach the decoder unannounced.
//
// Lines: 1 version, 2 database, 3 url, 4 defaults, 5 headers, 6 `    1: a`, 7 `    0x1: b`.
const twoRenderingsOfOneHeaderName = `version: 1
database:
  url: postgres://noty:pw@db.internal:5432/noty
defaults:
  headers:
    1: a
    0x1: b
listeners: []
`

// TestTheParserDoesNotCatchTwoRenderingsOfOneKey pins the gap this stage closes, so that a library
// upgrade which closes it upstream shows up here by name rather than as an unexplained duplicate
// diagnostic.
//
// It is the measurement behind Step 4's Implementation Note 17: stage B's duplicate detection
// compares what the parser read as text, so every spelling of a textual key is caught there and
// two renderings of one *number* are not.
func TestTheParserDoesNotCatchTwoRenderingsOfOneKey(t *testing.T) {
	caught := map[string]string{
		"one name written twice":           "insert: {}\ninsert: {}\n",
		"the same name single-quoted":      "insert: {}\n'insert': {}\n",
		"the same name double-quoted":      "insert: {}\n\"insert\": {}\n",
		"the same name tagged":             "insert: {}\n!!str insert: {}\n",
		"the same name written explicitly": "insert: {}\n? insert\n: {}\n",
		"the same name carrying an anchor": "insert: {}\n&k insert: {}\n",
	}
	missed := map[string]string{
		"sixteen written twice": "16: a\n0x10: b\n",
		"true written twice":    "true: a\nTrue: b\n",
	}

	for name, document := range caught {
		t.Run("caught: "+name, func(t *testing.T) {
			if _, diags := parseDocument(newSource("probe.yaml", []byte(document))); len(diags) != 1 {
				t.Errorf("%d diagnostics, want the parser's own duplicate-key error: %q", len(diags), messagesOf(diags))
			}
		})
	}
	for name, document := range missed {
		t.Run("missed: "+name, func(t *testing.T) {
			if _, diags := parseDocument(newSource("probe.yaml", []byte(document))); len(diags) != 0 {
				t.Errorf("the parser now reports %q; the gap this stage closes is gone, so re-derive whether it still needs closing",
					messagesOf(diags))
			}
		})
	}
}

// TestADuplicateTheParserMissesWouldOtherwiseFailTheDecoder is the other half of the justification:
// the gap is not cosmetic. Left alone, the mapping reaches Step 6's decode and fails there with no
// position at all, which is the outcome every stage of this package exists to prevent.
func TestADuplicateTheParserMissesWouldOtherwiseFailTheDecoder(t *testing.T) {
	root, diags := parseDocument(newSource("probe.yaml", []byte("16: a\n0x10: b\n")))
	if len(diags) != 0 {
		t.Fatalf("the parser refused the document, so this asserts nothing: %q", messagesOf(diags))
	}

	var decoded map[string]any
	err := yaml.NodeToValue(root, &decoded)

	if err == nil {
		t.Fatalf("the decoder accepted one key written twice and produced %v; the refusal below is no longer load-bearing", decoded)
	}
	if !strings.Contains(err.Error(), "duplicate key") {
		t.Errorf("the decoder failed with %v, want a duplicate-key error", err)
	}
}
