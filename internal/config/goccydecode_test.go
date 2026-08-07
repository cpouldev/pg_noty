package config

import (
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// The library behaviour stage G is built on, asserted as its own tests so that an upgrade fails here by
// name rather than surfacing as an unexplained diagnostic count somewhere in the product.
//
// Two findings of the Architecture's measured list are pinned here, and each names its identifier so a
// failure points at the measurement that stopped being true rather than at the code that trusted it.
//
// The types below are declared for these tests alone. They must not be the package's own wrappers: a
// pin that used them would measure this package's behaviour and call it the library's, so an upgrade
// that broke the library would be indistinguishable from a change in scalar.go.

// pinnedScalar is the minimum a NodeUnmarshaler on a scalar field can be: it records that it was called
// and returns nil, and nothing else.
//
// It records only the fact of the call. An earlier version also kept the node's rendering, which nothing
// read -- and a field nothing reads cannot fail, so it looked like evidence the pins did not have.
type pinnedScalar struct {
	called bool
}

func (p *pinnedScalar) UnmarshalYAML(ast.Node) error {
	p.called = true
	return nil
}

// pinnedBlock is the same for a mapping field, which is what V4's second half is measured through.
type pinnedBlock struct {
	called bool
}

func (p *pinnedBlock) UnmarshalYAML(ast.Node) error {
	p.called = true
	return nil
}

// TestGoccyLetsANilReturningScalarUnmarshalerMakeDecodeUnableToFail is **V8**, the keystone finding
// this whole step rests on: a NodeUnmarshaler on a scalar field receives the node, and by returning nil
// makes the enclosing decode succeed however unreadable the value was.
//
// Each row is a value that would fail a plain typed decode -- text where an int is declared, a duration
// unit Go does not have -- so a library that stopped consulting the unmarshaler would fail here rather
// than silently reinstating first-error-only reporting across the package.
func TestGoccyLetsANilReturningScalarUnmarshalerMakeDecodeUnableToFail(t *testing.T) {
	tests := []string{
		"value: 16\n",
		"value: abc\n",
		"value: 7d\n",
		"value: {a: 1}\n",
		"value: [a, b]\n",
	}

	for _, document := range tests {
		t.Run(document, func(t *testing.T) {
			var into struct {
				Value pinnedScalar `yaml:"value"`
			}

			if err := yaml.NodeToValue(parsedRoot(t, document), &into); err != nil {
				t.Fatalf("NodeToValue failed with %v; V8 no longer holds, so the never-fail decode "+
					"guarantee ADR-2 rests on is gone", err)
			}
			if !into.Value.called {
				t.Error("the unmarshaler was not called, so the node never reached it")
			}
		})
	}
}

// TestGoccyReadsAnInterpolatedIntegerButNotAnInterpolatedBoolean is the measurement that decided how the
// wrappers convert: the library's typed decode is **not** uniform across a string node. It reads "16"
// into an int and refuses "true" into a bool.
//
// This is why conversion.go takes one text and converts it itself rather than calling NodeToValue once
// per target type. Both halves are pinned: a library that closed the gap would make the wrappers
// needlessly indirect and should be noticed, and one that opened a new gap -- refusing "16" into an int
// -- would break AC #5 through a path no product test names.
func TestGoccyReadsAnInterpolatedIntegerButNotAnInterpolatedBoolean(t *testing.T) {
	// The shape stage D leaves behind: the author wrote a reference, so the node is a string node
	// whatever the environment put in it.
	interpolated := func(t *testing.T, resolved string) ast.Node {
		t.Helper()

		root := parsedRoot(t, "value: ${V}\n")
		written, isText := nodeIn(t, root, "$.value").(*ast.StringNode)
		if !isText {
			t.Fatalf("a bare ${V} no longer parses to a string node, so this measurement is about "+
				"a different shape: %T", nodeIn(t, root, "$.value"))
		}
		written.Value = resolved
		return written
	}

	t.Run("an integer is read", func(t *testing.T) {
		var held int
		if err := yaml.NodeToValue(interpolated(t, "16"), &held); err != nil || held != 16 {
			t.Fatalf("read %d with %v, want 16: AC #5's success half now needs a different mechanism", held, err)
		}
	})

	t.Run("a boolean is refused", func(t *testing.T) {
		var held bool
		err := yaml.NodeToValue(interpolated(t, "true"), &held)

		if err == nil {
			t.Fatalf("the library now reads an interpolated boolean into a bool (%t); conversion.go's "+
				"one-text mechanism is no longer required and its comment should be re-derived", held)
		}
	})

	t.Run("a bad duration is refused without a position", func(t *testing.T) {
		// **V9**, and the reason R40's diagnostic has to be the wrapper's. The library reads a valid
		// duration from a string node -- so a native `time.Duration` field would have worked for every
		// correct file -- and refuses `7d` with `time`'s own error, which carries no line, no column
		// and no path. A field that relied on it would report the one thing this package exists to
		// prevent: a mistake with no position.
		var valid time.Duration
		if err := yaml.NodeToValue(interpolated(t, "10s"), &valid); err != nil || valid != 10*time.Second {
			t.Fatalf("the library read %v with %v, want 10s; V9's first half no longer holds", valid, err)
		}

		var refused time.Duration
		err := yaml.NodeToValue(interpolated(t, "7d"), &refused)
		if err == nil {
			t.Fatal("the library now accepts a day unit, so the accepted-unit set is no longer time.ParseDuration's")
		}

		// "Positionless" is asserted as what the error *is*, not as how it renders. The previous form
		// checked that the text held no `|`, which is goccy's snippet-rendering convention -- so a version
		// that attached a position and rendered it any other way left the premise false, R40 duplicable,
		// and the suite green. That is the class this step's own new rule governs.
		//
		// What is asserted instead: the library hands back time.ParseDuration's own error, unwrapped.
		// Measured on v1.19.2 the value is a bare *errors.errorString whose text is byte-identical to the
		// standard library's -- so any wrapping at all, positional or otherwise, breaks this equality.
		_, fromTheStandardLibrary := time.ParseDuration("7d")
		if fromTheStandardLibrary == nil {
			t.Fatal("time.ParseDuration now accepts a day unit, so this measurement is about nothing")
		}
		if err.Error() != fromTheStandardLibrary.Error() {
			t.Errorf("the library returned %q where time.ParseDuration returns %q; it now wraps the "+
				"failure, so re-derive whether R40's diagnostic can still come only from the wrapper",
				err, fromTheStandardLibrary)
		}
	})
}

// TestGoccyContinuesDecodingSiblingsAfterAFailure is what makes the stop-signal test in decode_test.go
// falsifiable: a decoder that abandoned the document at its first failure would leave the sibling value
// unread, and "stage G did not run" would be indistinguishable from "stage G ran and found nothing".
func TestGoccyContinuesDecodingSiblingsAfterAFailure(t *testing.T) {
	type plain struct {
		Inner pinnedScalar `yaml:"inner"`
	}
	var into struct {
		First pinnedScalar `yaml:"first"`
		Sub   plain        `yaml:"sub"`
		Last  pinnedScalar `yaml:"last"`
	}

	err := yaml.NodeToValue(parsedRoot(t, "first: a\nsub: text\nlast: z\n"), &into)

	if err == nil {
		t.Fatal("the decoder accepted a scalar where a struct is declared, so this measurement is vacuous")
	}
	if !into.First.called || !into.Last.called {
		t.Errorf("the decoder stopped at the failure (first called %t, last called %t); a value written "+
			"after a failing sibling is no longer read, so re-derive how the stage-F stop is observed",
			into.First.called, into.Last.called)
	}
}
