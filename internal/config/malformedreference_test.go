package config

import (
	"fmt"
	"strings"
	"testing"
)

// What a spelling the grammar refuses costs: it never reaches a configuration, and it never
// survives as literal text (envreference.go, envexpansion.go).
//
// The table beside this one (envreference_test.go) says what each refused form does to its own
// scalar; these two say what the refusal is worth at the two boundaries that matter -- the
// public entry point, and an unbounded domain of reference text.

// TestAMalformedReferenceNeverReachesTheConfiguration is the outcome half of AC #3's
// rejecting forms, asserted through the public entry point: whatever the node keeps, the
// run produces diagnostics and no configuration, so no malformed reference can be shipped
// into DDL or an HTTP destination as literal text.
func TestAMalformedReferenceNeverReachesTheConfiguration(t *testing.T) {
	for _, written := range []string{"${1VAR}", "${}", "${VAR", "${UNSET_AND_UNDEFAULTED}"} {
		t.Run(written, func(t *testing.T) {
			cfg, warnings, errs := Parse([]byte("value: "+written+"\n"), interpolationFixture, MapEnv(nil))

			if len(errs) == 0 {
				t.Fatalf("Parse() accepted %q", written)
			}
			if cfg != nil {
				t.Error("Parse() returned a configuration alongside an interpolation diagnostic")
			}
			if len(warnings) != 0 {
				t.Errorf("Parse() returned %d warnings from a stage-D stop, want none", len(warnings))
			}
			for _, diag := range errs {
				if diag.Rule != RuleInterpolate {
					t.Errorf("Rule = %q, want %q", diag.Rule, RuleInterpolate)
				}
			}
		})
	}
}

// TestAnOccurrenceShorterThanTheDelimiterIsRefusedRatherThanPanicking covers readReference's
// precondition, which was otherwise only a doc comment.
//
// Both callers check the prefix before calling, so nothing reaches this today. A doc comment
// does not fail when a third caller forgets, and an index panic inside the loader is the worst
// answer this package can give -- so the precondition is a refusal, and the refusal is
// asserted rather than merely written.
func TestAnOccurrenceShorterThanTheDelimiterIsRefusedRatherThanPanicking(t *testing.T) {
	for _, text := range []string{"", "$"} {
		t.Run(fmt.Sprintf("%q", text), func(t *testing.T) {
			read := readReference(text)

			if read.refusal != unterminatedRefusal {
				t.Errorf("refusal = %q, want %q", read.refusal, unterminatedRefusal)
			}
			if read.width != len(text) {
				t.Errorf("width = %d, want %d, the whole of what there was to read", read.width, len(text))
			}
			if read.name != "" {
				t.Errorf("name = %q, want none: nothing this short names a variable", read.name)
			}
		})
	}
}

// FuzzOneOccurrenceEmitsNoUnansweredReference is the closure claim over the dimension the
// opacity target does not vary: the *reference text* an author can write, rather than the
// environment value a reference resolves to. Between them the two cover both byte sources
// that reach a value, which is what the closure rule answers separately.
//
// The environment is empty on purpose. With nothing set, every occurrence either faults or
// resolves from its own default -- author-written text, which is exactly the source that must
// still be owed the grammar, and the source the table's `${A:-${B}}` row covers by hand.
//
// The domain is one whole occurrence and nothing else, and that bound is the property's
// meaning rather than a convenience. Fuzzing this claim over a whole scalar reported
// `${A:-$}{`, where the substituted `$` and the literal `{` after the occurrence spell `${`
// across the join between them: concatenation, not an unanswered reference, and no more
// avoidable than the same two characters arriving in one environment value. That input is
// committed as a seed so the boundary stays recorded rather than rediscovered.
//
// It discriminates rather than restating the code: emitting a default verbatim -- the shape
// that let `${A:-${B}}` ship the literal `${B}` with no diagnostic -- fails it on the third
// seed below.
func FuzzOneOccurrenceEmitsNoUnansweredReference(f *testing.F) {
	for _, written := range []string{
		"${NAME}",
		"${NAME:-fallback}",
		"${A:-${B}",
		"${A:-$${B}",
		"${A:-x}",
		"${${A}",
		"${}",
		"${NAME",
	} {
		f.Add(written)
	}

	f.Fuzz(func(t *testing.T, written string) {
		if !strings.HasPrefix(written, referenceOpen) || readReference(written).width != len(written) {
			return
		}

		result := expand(written, MapEnv(nil))
		if len(result.faults) != 0 || strings.Contains(written, escapeSentinel) {
			// The escape is the grammar's own way of asking for a literal `${`, and a default
			// is owed the whole grammar -- so `${A:-$${B}` yielding `${B` is the answer, not a
			// residue. That is the table's exclusion, applied here for the same reason.
			return
		}
		if strings.Contains(result.text, referenceOpen) {
			t.Errorf("%q became %q, which still holds %q and reported nothing",
				written, result.text, referenceOpen)
		}
	})
}
