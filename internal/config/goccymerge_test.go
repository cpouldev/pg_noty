package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// Finding V5, kept a measurement rather than a memory.
//
// Both of the library's own merge-key paths are pinned here, and neither is used by the loader:
// this file is the evidence for normalize.go's conformance comment, so an upgrade in either
// direction fails a named test about the library instead of surfacing as a product bug. The
// second path is the dangerous one -- it does not fail, it silently reverses precedence.

// mergedRetry is the shape V5 was measured against: a mapping that merges an anchored block and
// writes one of its keys directly. Both are decoded, so the anchor's own values are visible
// beside the merged ones and a test cannot mistake one for the other.
type mergedRetry struct {
	Base  retrySettings `yaml:"base"`
	Retry retrySettings `yaml:"retry"`
}

type retrySettings struct {
	MaxAttempts int    `yaml:"max_attempts"`
	Backoff     string `yaml:"backoff"`
}

// The two orderings are mergekey_test.go's own constants, read here rather than copied. What a
// pin is for is measuring the library on the very document the product is asserted against, so
// the correspondence has to be the same declaration: two copies agree until one is edited, and
// nothing then fails.

// TestGoccyRefusesToDecodeAMergeKeyIntoAStruct pins the first half of V5: the library's default
// path does not merge at all, it fails, because the merged key and the direct key look to it
// like a duplicate. Measured in both orderings, since the error names a different token in each.
func TestGoccyRefusesToDecodeAMergeKeyIntoAStruct(t *testing.T) {
	orderings := map[string]string{
		"the merge key written above the direct key": directKeyBelowTheMergeKey,
		"the merge key written below the direct key": directKeyAboveTheMergeKey,
	}

	for name, document := range orderings {
		t.Run(name, func(t *testing.T) {
			var decoded mergedRetry

			err := yaml.NodeToValue(parsedRoot(t, document), &decoded)

			if err == nil {
				t.Fatalf("NodeToValue accepted a merge key and produced %+v; the library now merges, so normalize.go's comment is stale", decoded)
			}
			if !strings.Contains(err.Error(), `duplicate key "max_attempts"`) {
				t.Errorf("the refusal reads %q, want it to name the duplicate key", err)
			}
		})
	}
}

// TestGoccyWithDuplicateKeysAllowedFollowsDocumentOrderRatherThanMergePrecedence pins the second
// half of V5, and it is the half that decides the design: the option that makes the decode
// succeed makes it succeed *wrongly* in one of the two orderings.
//
// A direct key written below the merge key survives, and the identical document with the two
// lines swapped loses it -- so a configuration can be silently overridden by the defaults it
// meant to override, which is the failure AC #32 exists to prevent.
func TestGoccyWithDuplicateKeysAllowedFollowsDocumentOrderRatherThanMergePrecedence(t *testing.T) {
	tests := []struct {
		name     string
		document string
		want     int
	}{
		{
			name:     "the direct key written last wins, which is right by accident",
			document: directKeyBelowTheMergeKey,
			want:     10,
		},
		{
			name:     "the direct key written first loses, which YAML says it must not",
			document: directKeyAboveTheMergeKey,
			want:     5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var decoded mergedRetry

			if err := yaml.NodeToValue(parsedRoot(t, tc.document), &decoded, yaml.AllowDuplicateMapKey()); err != nil {
				t.Fatalf("NodeToValue with duplicate keys allowed failed: %v", err)
			}

			if decoded.Retry.MaxAttempts != tc.want {
				t.Errorf("retry.max_attempts decoded to %d, want the %d this library version produces; V5 no longer holds",
					decoded.Retry.MaxAttempts, tc.want)
			}
		})
	}
}

// TestGoccyMarksAMergeKeyOnANodeTypeOfItsOwn pins how `<<` is recognised, which is the detection
// skill Pattern 4 prescribes and the reason the expansion never compares a key's text to "<<".
//
// The measurement that matters is the second assertion. Pattern 4's own snippet reaches the
// predicate through `mv.Key.(*ast.StringNode)`, and this library version does not give `<<` a
// string node at all -- so a check written that way compiles, runs and never fires, which is
// indistinguishable from a document that uses no merge keys. The predicate is reached through
// ast.MapKeyNode instead, which every key shape implements.
func TestGoccyMarksAMergeKeyOnANodeTypeOfItsOwn(t *testing.T) {
	root := parsedRoot(t, "base: &b {a: 1}\nuse:\n  <<: *b\n")
	merge := firstEntryOf(t, valueOfEntry(root, 1))

	if !merge.Key.IsMergeKey() {
		t.Errorf("the key of `<<: *b` is %T and does not report itself as a merge key", merge.Key)
	}
	if _, isString := merge.Key.(*ast.StringNode); isString {
		t.Error("`<<` now parses to an *ast.StringNode, so skill Pattern 4's own type assertion would fire; re-read the detection")
	}
	if merge.Key.String() != mergeKeyText {
		t.Errorf("the merge key renders as %q, want %q", merge.Key.String(), mergeKeyText)
	}
}

// TestGoccyResolvesNoAliasWhileParsing pins V7's consequence for this stage: an alias arrives as
// a node holding a name, and an alias naming no anchor at all is not a parse error. Both are why
// resolution and its two refusals are ours.
func TestGoccyResolvesNoAliasWhileParsing(t *testing.T) {
	root := parsedRoot(t, "base: &b {a: 1}\nuse: *b\nbroken: *nowhere\n")

	resolved, isAlias := valueOfEntry(root, 1).(*ast.AliasNode)
	if !isAlias {
		t.Fatalf("`use: *b` parsed to %T, so the parser now resolves aliases itself", valueOfEntry(root, 1))
	}
	if got := resolved.Value.String(); got != "b" {
		t.Errorf("the alias holds %q, want the anchor name it was written with", got)
	}
	if _, isAlias := valueOfEntry(root, 2).(*ast.AliasNode); !isAlias {
		t.Errorf("`*nowhere` parsed to %T; an alias naming no anchor is still an alias node",
			valueOfEntry(root, 2))
	}
}
