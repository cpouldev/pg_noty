package config

import (
	"testing"

	"github.com/goccy/go-yaml"
)

// Stage G as a stage: that it cannot fail whatever the document, that it contributes nothing but the
// conversion failures its wrappers recorded, that it passes the library no strictness option, and
// that it does not run at all on a document stage F called unreadable.
//
// The wrappers themselves are scalar_test.go's subject and the three presence states are
// presence_test.go's.

// stageG runs the pipeline's stages D through G over text and answers the raw tree together with the
// diagnostics decode recorded.
//
// The earlier stages run in the pipeline's own order because stage G is promised what they leave
// behind: substituted values, resolved aliases and a shape the contract admits. A fixture that does
// not reach stage G fails here rather than quietly asserting nothing about it.
func stageG(t *testing.T, text string) (*rawConfig, Errors) {
	t.Helper()

	src, root, diags := stageE(t, text, corpusVariables)
	if len(diags) != 0 {
		t.Fatalf("the fixture does not reach stage G: %q\nsource:\n%s", messagesOf(diags), text)
	}
	if shape, decodable := checkShape(src, root); !decodable {
		t.Fatalf("stage F called the document unreadable, so stage G never runs on it: %q\nsource:\n%s",
			messagesOf(shape), text)
	}
	return decodeDocument(src, root)
}

// decodedConfig is the raw tree of a document stage G decoded with nothing to report, for the many
// cases whose subject is the decoded value rather than a diagnostic.
func decodedConfig(t *testing.T, text string) *rawConfig {
	t.Helper()

	decoded, diags := stageG(t, text)
	if len(diags) != 0 {
		t.Fatalf("decoding reported %q\nsource:\n%s", messagesOf(diags), text)
	}
	return decoded
}

// TestDecodeAnswersNilForEveryShapeADocumentCanHold is the stage-level half of the never-fail
// guarantee, and it is quantified over the shapes rather than over one example.
//
// The case list is the one stage F already uses: propertySpellings enumerates every node property a
// declared value or key may carry, and every row is a complete configuration the pipeline accepts. It
// is reused rather than restated so that a spelling added for stage F cannot pass here by not being
// tried.
//
// Those rows are the ones that matter: `database: !!map {url: x}` fails yaml.NodeToValue outright when
// the target is a plain struct, and stage F accepts the document -- so before rawcontainer.go's peel
// this test failed on four rows while the whole rest of the suite stayed green.
func TestDecodeAnswersNilForEveryShapeADocumentCanHold(t *testing.T) {
	if len(propertySpellings) == 0 {
		t.Fatal("no property spellings, so this test would pass vacuously")
	}

	for spelling, document := range propertySpellings {
		t.Run(spelling, func(t *testing.T) {
			decoded, diags := stageG(t, document)

			if len(diags) != 0 {
				t.Errorf("decoding reported %q on a legal document", messagesOf(diags))
			}
			// Without this the rows would pass for a decode that filled nothing at all, which is
			// exactly what a container wrapper that stopped peeling would do.
			if !decoded.Database.Set || !decoded.Database.Value.URL.Valid() {
				t.Errorf("the database block decoded to %+v; the value beneath the property was not read",
					decoded.Database.Value.URL)
			}
		})
	}
}

// TestStageGIsOnlyEverHandedABareMappingRoot pins the invariant decode.go's never-fail guarantee
// inherits from stage C, which nothing else asserts: the top-level call is the one position in the
// package that does *not* peel node properties, and it is safe only because no root carrying any can
// reach it.
//
// Stage C's refusal is asserted for both node properties a root can carry; what the decoder does with
// each is measured, and the two differ. A **tag** is the load-bearing case: NodeToValue refuses `!!map`
// at the root outright, so were stage C to start accepting one the decode would not fail loudly -- the
// error is discarded -- it would fill *nothing*, leaving every value absent and the whole document
// reported clean. An **anchor** the library reads straight through, so that row pins the near-miss
// rather than a dependency, and keeps this test from reading as "any node property on a root breaks the
// decoder", which is not what was measured.
func TestStageGIsOnlyEverHandedABareMappingRoot(t *testing.T) {
	tests := []struct {
		property           string
		refusedByTheDecode bool
	}{
		{property: "!!map", refusedByTheDecode: true},
		{property: "&whole", refusedByTheDecode: false},
	}

	for _, tc := range tests {
		t.Run(tc.property, func(t *testing.T) {
			document := tc.property + "\nversion: 1\ninstance: noty\n"

			// What the decoder would do with such a root, were one ever handed to it.
			var decoded rawConfig
			err := yaml.NodeToValue(parsedRoot(t, document), &decoded)

			if refused := err != nil; refused != tc.refusedByTheDecode {
				t.Errorf("NodeToValue refused a %s root = %t (%v), want %t; the asymmetry decode.go's "+
					"unpeeled top-level call rests on has changed", tc.property, refused, err,
					tc.refusedByTheDecode)
			}
			if tc.refusedByTheDecode && decoded.Version.Set {
				t.Error("the decoder filled a field of a root it refused, so a silent partial decode is possible")
			}

			// The stop, which is what makes the row above unreachable through the pipeline either way.
			_, refused := parseDocument(newSource("listeners.yaml", []byte(document)))
			if len(refused) != 1 {
				t.Fatalf("stage C reported %q for a %s root, want exactly one refusal",
					messagesOf(refused), tc.property)
			}
			if refused[0].Rule != RuleDocument {
				t.Errorf("Rule = %q, want %q: a root carrying node properties reaches stage G",
					refused[0].Rule, RuleDocument)
			}
		})
	}
}
