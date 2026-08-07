package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// AC #4: a substituted value is opaque bytes, and the document it lands in still reports every
// position it reported before.
//
// The two claims share a file because they have one cause -- substituting into a parsed node
// rather than into the source text -- and one adversary: a value carrying a colon, a `#`, a
// quote, a newline and the text `${`, every one of which would change the document's shape
// if it were spliced in before parsing.
//
// The positions a document reports are read by an oracle written without reference to this
// stage (nodereach_test.go), and the eight places a value can be written are enumerated
// separately so both claims can be quantified over them (referencelayout_test.go).

// adversarialValue is AC #4's value: a colon, a comment marker, a quote, a newline and the
// text of a reference. Every one of them would change the document's shape if interpolation
// were applied to raw text before parsing, which is why the design applies it to parsed
// nodes.
const adversarialValue = "p@ss:word #1 \"quoted\"\nsecond line ${NOT_A_REFERENCE}"

// TestTheCommittedAdversarialFixtureSubstitutesByteIdentically is AC #4 on a real
// configuration rather than a document built for the assertion: a committed valid fixture,
// loaded against the corpus environment the rest of that corpus is loaded against.
//
// It is the fixture's presence in testdata/valid that carries most of the weight -- it makes
// the same document a case of TestValidFixturesRenderNothing, so a value carrying a colon, a
// `#`, a quote, a newline and `${` has to leave the file loading clean as well as arriving
// intact.
func TestTheCommittedAdversarialFixtureSubstitutesByteIdentically(t *testing.T) {
	data := readFixtureBytes(t, filepath.Join(validCorpus, "interpolated_adversarial_value.yaml"))

	assertOpaqueSubstitution(t, string(data),
		"$.listeners[0].destination.headers.'X-Adversarial'", corpusEnvironment(), adversarialValue)
}

// assertOpaqueSubstitution is the shared oracle for opacity: the value arrives
// byte-identically, nothing about it is reported, the source keeps its bytes and its line
// count, and every position the document reports is unmoved.
func assertOpaqueSubstitution(t *testing.T, document, path string, env EnvLookup, want string) {
	t.Helper()

	src := newSource(interpolationFixture, []byte(document))
	root, diags := parseDocument(src)
	if len(diags) != 0 {
		t.Fatalf("the fixture does not reach stage D: %+v\nsource:\n%s", diags, document)
	}

	linesBefore := len(src.lines)
	bytesBefore := string(src.CloneBytes())
	positionsBefore := reportedPositions(src, root)

	_, faults := interpolate(src, root, env)

	if len(faults) != 0 {
		t.Fatalf("substituting an opaque value reported %q", messagesOf(faults))
	}
	if got := textIn(t, root, path); got != want {
		t.Errorf("value = %q, want %q", got, want)
	}
	if got := len(src.lines); got != linesBefore {
		t.Errorf("the document now has %d lines, want %d; a value's newlines must not become the document's",
			got, linesBefore)
	}
	if got := string(src.CloneBytes()); got != bytesBefore {
		t.Errorf("source bytes changed:\n%q\nwant:\n%q", got, bytesBefore)
	}
	if got := reportedPositions(src, root); !slices.Equal(got, positionsBefore) {
		t.Errorf("positions moved:\n%v\nwant:\n%v", got, positionsBefore)
	}

	assertOpaqueThroughParse(t, document, env)
}

// assertOpaqueThroughParse runs the same document through the public entry point, so opacity
// is a claim about what a caller receives rather than about the stage seam alone.
//
// Two things are only observable here. A caller's own byte slice goes in, and is compared
// afterwards: Parse takes a slice, and a stage that wrote a substituted value through it would
// leave the environment's bytes in the caller's memory rather than in the tree. And a run whose
// references all resolve carries the value past stage D without a word about it, and warns about
// nothing, which is what makes "the value produced no diagnostic" true of the pipeline rather
// than of the stage seam alone (AC #23 case 3).
//
// The diagnostics are scoped to stage D rather than counted, and that scoping arrived with stage
// F. The layouts these documents are built from are *fragments* -- one reference in one syntactic
// position -- rather than configurations, so stage F rightly reports their shape: `value:..` is a
// key the contract declares nowhere. Counting to zero here would therefore mean either weakening
// stage F or rewriting nine layouts into nine valid configurations, and neither is what AC #4 is
// about. What AC #4 is about is that nothing is reported *about the value*, which is what naming
// stage D asserts.
func assertOpaqueThroughParse(t *testing.T, document string, env EnvLookup) {
	t.Helper()

	callers := []byte(document)
	_, warnings, errs := Parse(callers, interpolationFixture, env)

	for _, diag := range errs {
		if diag.Rule == RuleInterpolate {
			t.Errorf("Parse reported %q about a reference that resolves", diag.Msg)
		}
	}
	if len(warnings) != 0 {
		t.Errorf("Parse returned %d warnings; W1 and W2 are computed in stages H and I", len(warnings))
	}
	if string(callers) != document {
		t.Errorf("Parse wrote through the caller's bytes:\n%q\nwant:\n%q", callers, document)
	}
}

// TestARenderedStageDDiagnosticShowsTheReferenceAndNotItsValue is ADR-5 at this stage: the
// renderer reads the immutable source, so what a caller sees is the text on disk. The
// environment holds a value that would be unmistakable if it leaked.
func TestARenderedStageDDiagnosticShowsTheReferenceAndNotItsValue(t *testing.T) {
	const document = "database:\n  url: ${DATABASE_URL}\n  schema: ${DATABASE_SCHEMA}\n"
	const resolved = "postgres://noty:s3cret-that-must-not-render@db.internal/noty"

	_, _, errs := Parse([]byte(document), interpolationFixture, MapEnv(map[string]string{"DATABASE_URL": resolved}))

	rendered := errs.Render([]byte(document))
	if !strings.Contains(rendered, "${DATABASE_SCHEMA}") {
		t.Errorf("the rendered diagnostic does not quote the reference:\n%s", rendered)
	}
	if strings.Contains(rendered, "s3cret-that-must-not-render") {
		t.Errorf("a substituted value reached rendered output:\n%s", rendered)
	}
	if strings.Contains(rendered, resolved) {
		t.Errorf("the resolved database URL reached rendered output:\n%s", rendered)
	}
}
