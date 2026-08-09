package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// The library's tag asymmetry, which is the measurement rawcontainer.go exists for and the one that makes
// conversion.go's own peel redundant: a tag is read past when the target is a scalar and refused when the
// target is a struct.
//
// It is not one of the Architecture's numbered findings, which is why both halves are recorded in full: a
// document writing `!!map` is legal YAML that stage F accepts, so before the peel a file the whole
// pipeline called clean would have decoded to nothing.

// TestGoccyRefusesATaggedMappingWhereAStructIsExpected is the measurement rawcontainer.go exists for. It
// is not in the Architecture's numbered list, which is why it is recorded here in full: `!!map` on a
// value the decoder must fill into a struct fails, while the same tag handed to a NodeUnmarshaler does
// not -- and stage F accepts both, because a tag is a claim about a value rather than a value of its own.
func TestGoccyRefusesATaggedMappingWhereAStructIsExpected(t *testing.T) {
	type plain struct {
		Inner pinnedScalar `yaml:"inner"`
	}

	t.Run("into a struct", func(t *testing.T) {
		var into struct {
			Sub plain `yaml:"sub"`
		}

		err := yaml.NodeToValue(parsedRoot(t, "sub: !!map {inner: a}\n"), &into)

		if err == nil {
			t.Fatal("the decoder now fills a struct from a tagged mapping; rawcontainer.go's peel is no " +
				"longer load-bearing and its comment should be re-derived")
		}
		if !strings.Contains(err.Error(), "tag was used where mapping is expected") {
			t.Errorf("the decoder failed with %v, want the tag refusal this package peels around", err)
		}
	})

	t.Run("into an unmarshaler", func(t *testing.T) {
		var into struct {
			Sub pinnedBlock `yaml:"sub"`
		}

		if err := yaml.NodeToValue(parsedRoot(t, "sub: !!map {inner: a}\n"), &into); err != nil {
			t.Fatalf("the decoder refused a tagged mapping handed to an unmarshaler with %v; the peel "+
				"rawcontainer.go performs would then not be enough", err)
		}
	})
}

// TestGoccyReadsAScalarThroughItsOwnTag is the asymmetry the two container carriers exist for, stated
// from the other side: the library peels a tag when the target is a scalar and refuses one when the
// target is a struct.
//
// It is pinned because the first half makes conversion.go's own peel redundant -- found by mutation --
// and the second is why rawcontainer.go's is not. An upgrade that levelled the two in either direction
// changes which of this package's peels are load-bearing, and should be read here first.
func TestGoccyReadsAScalarThroughItsOwnTag(t *testing.T) {
	tagged := nodeIn(t, parsedRoot(t, "value: !!str tagged\n"), "$.value")
	if _, isTag := tagged.(*ast.TagNode); !isTag {
		t.Fatalf("a tagged value no longer arrives as a tag node (%T), so this measurement is about a "+
			"different shape", tagged)
	}

	var held string
	if err := yaml.NodeToValue(tagged, &held); err != nil || held != "tagged" {
		t.Fatalf("read %q with %v, want the value beneath the tag; conversion.go's peel has become "+
			"load-bearing and its comment should say so", held, err)
	}
}

// TestGoccyEndsABlockScalarAtALoneCarriageReturn is the measurement source.go's carriageReturnSplits
// rests on, and the one that decided the shape of the leak fix.
//
// A lone carriage return ends a block scalar exactly as a dedent does: the bytes after it parse as a key
// of their own, so no sensitive key governs them and nothing in the contract's own vocabulary covers
// them. That is why the redactor cannot decide the case from the parse, and has to know that those bytes
// sit on the physical line above.
func TestGoccyEndsABlockScalarAtALoneCarriageReturn(t *testing.T) {
	tests := map[string]struct {
		document string
		wantKeys int
	}{
		"a carriage return inside a block scalar": {document: "k:\n  - |\n    aaa\rbbb: ccc\n", wantKeys: 2},
		"a line feed inside a block scalar":       {document: "k:\n  - |\n    aaa\n    bbb: ccc\n", wantKeys: 1},
		"a dedent ends the block":                 {document: "k:\n  - |\n    aaa\nbbb: ccc\n", wantKeys: 2},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			root := parsedRoot(t, tc.document)

			mapping, isMapping := root.(*ast.MappingNode)
			if !isMapping {
				t.Fatalf("root is %T, want a mapping", root)
			}
			if got := len(mapping.Values); got != tc.wantKeys {
				t.Errorf("the document parsed to %d root keys, want %d; the block scalar's extent changed, "+
					"so re-derive why redaction cannot decide a carriage-return split from the parse", got, tc.wantKeys)
			}
		})
	}
}
