package config

import (
	"fmt"
	"testing"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// The scalar wrappers' never-fail guarantee over an unbounded input domain, one target per wrapper.
// StrList has the same guarantee over a different domain -- a generated sequence and independently
// generated element shapes -- in scalarlistfuzz_test.go.
//
// Five hand-picked adversarial inputs (scalar_test.go) prove each wrapper answers nil for the value its
// author most plausibly gets wrong. They cannot prove it for *arbitrary* bytes, and arbitrary bytes are
// what a configuration value is: after stage D a value holds whatever the environment put in it, and the
// environment is outside this program. So the domain the invariant is quantified over is generated.
//
// **The invariant is four claims, and one of them does not generalise across the five wrappers.**
// UnmarshalYAML returns nil; it does not panic; a value it could not read is reported rather than
// swallowed; and no two of its reports land on one position. The last two are what make this more than a
// crash test: a wrapper that swallowed a value would satisfy the first two and lose the diagnostic its
// author needs, and one reporting twice at one position would double-report through de-duplication's blind
// spot -- two refusals of one value at one position with different wording are two diagnostics.
//
// StrList cannot share the scalar generator: parsing one placeholder and mutating its StringNode always
// hands the wrapper a scalar, even when the bytes happen to look like `[nested]` or `*alias`. Its target
// therefore parses a real two-element sequence, generates each element's shape independently, and only
// then installs opaque bytes into the already-parsed scalar holders.
//
// **Exploration is behind `-fuzz`**, as the golden corpus's regeneration is behind `-update`: a default
// `go test` run executes exactly the scalar seeds below, so the suite stays deterministic and hermetic. The seeds
// are the classes a reader should be able to see covered without running anything: a plain value of each
// type, the value of a *different* type, YAML punctuation, a container, an empty string, and bytes that
// are not YAML at all.

// fuzzSeeds is the committed seed corpus, shared by all four scalar targets so that each wrapper meets every
// class rather than only the ones its own author thought of.
//
// Each entry is the *value* text; the target writes it into a document, because a wrapper is only ever
// handed a node the parser produced and a hand-built node could not reproduce the parser's own reading of
// a scalar.
var fuzzSeeds = []string{
	"16", "0x10", "-1", "1.5",
	"true", "TRUE", "false",
	"10s", "7d", "1h30m",
	"noty", "", `""`, "'quoted'",
	"[a, b]", "[]", "{a: 1}", "{}",
	"null", "~", ".inf", ".nan",
	"#", ":", "- ", "*a", "&a x", "!!str x", "? x",
	"a: b: c", "\t", "\x00", "\U0001F600", "line1\nline2",
}

// FuzzStrNeverFails, and its three scalar siblings below, are one target per wrapper. They are separate functions
// rather than one target taking a selector because `go test -fuzz` explores exactly one target at a time:
// a selector would leave four wrappers unexplored in every run that used it.
func FuzzStrNeverFails(f *testing.F) {
	fuzzWrapper(f, func() decodedValue { return &Str{} })
}

func FuzzIntNeverFails(f *testing.F) {
	fuzzWrapper(f, func() decodedValue { return &Int{} })
}

func FuzzBoolNeverFails(f *testing.F) {
	fuzzWrapper(f, func() decodedValue { return &Bool{} })
}

func FuzzDurNeverFails(f *testing.F) {
	fuzzWrapper(f, func() decodedValue { return &Dur{} })
}

// fuzzWrapper seeds and runs one wrapper's target. The four claims are asserted here, once, so the five
// targets cannot drift into asserting different things about the same guarantee.
func fuzzWrapper(f *testing.F, fresh func() decodedValue) {
	for _, seed := range fuzzSeeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, written string) {
		src, node := generatedValue(t, written)

		held, pass := fresh(), newDecodePass(src)
		failed, panicked := wrapperReading(held, node, pass)

		if panicked != nil {
			t.Fatalf("a wrapper panicked on %q: %v; the guarantee is that arbitrary bytes are refused, "+
				"not that they crash the program", written, panicked)
		}
		if failed != nil {
			t.Fatalf("UnmarshalYAML(%q) returned %v; every wrapper must answer nil for every input", written, failed)
		}

		switch {
		case held.Valid() && len(pass.diags) != 0:
			t.Fatalf("read %q and recorded %q; a value it read must record nothing", written, messagesOf(pass.diags))
		case !held.Valid() && len(pass.diags) == 0:
			t.Fatalf("could not read %q and recorded nothing; the value is silently zeroed and its author is "+
				"told about a mistake they did not make instead", written)
		case !held.Valid() && len(pass.diags) != 1:
			t.Fatalf("could not read %q and recorded %d diagnostics %q; a scalar converts or does not, so it "+
				"has nothing to accumulate over and one failure costs exactly one diagnostic",
				written, len(pass.diags), messagesOf(pass.diags))
		}
	})
}

// positionSharedByTwo is a position two diagnostics both anchor on, and whether there is one.
//
// Position rather than message, because that is the shape de-duplication cannot see: it collapses
// identical diagnostics, so two *differently worded* refusals of one token both survive it.
func positionSharedByTwo(diags Errors) (string, bool) {
	seen := make(map[string]bool, len(diags))
	for _, diag := range diags {
		at := fmt.Sprintf("%s:%d:%d", diag.File, diag.Line, diag.Col)
		if seen[at] {
			return at, true
		}
		seen[at] = true
	}
	return "", false
}

// wrapperReading hands node to held and resolves it, reporting the error the wrapper answered and
// whatever it panicked with.
//
// The panic is recovered here rather than left to the test harness, which would fail the run anyway:
// what the recovery adds is attribution. A bare harness crash names the line that panicked, and both
// calls below run library code as well as this package's, so the message has to say that a *wrapper*
// panicked and on which input -- otherwise the third of the three invariants stated at the top of this
// file is asserted by nothing that can name it.
//
// Both calls are guarded together because both are the wrapper's own code, and either panicking is the
// same defect: a value the wrapper met and could not survive.
func wrapperReading(held decodedValue, node ast.Node, pass *decodePass) (failed error, panicked any) {
	defer func() { panicked = recover() }()

	failed = held.UnmarshalYAML(node)
	held.resolve(pass)
	return failed, nil
}

// generatedValue reproduces stage D's real boundary: parse a safe placeholder, then replace the
// already-parsed string node's value with opaque environment bytes. Re-parsing written would test
// YAML grammar and discard precisely the newlines, NULs and marker-shaped values interpolation can
// place here.
func generatedValue(t testing.TB, written string) (*source, ast.Node) {
	t.Helper()

	const document = "value: ${FUZZ_VALUE}\n"
	file, err := parser.ParseBytes([]byte(document), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse the fixed fuzz template: %v", err)
	}
	mapping := file.Docs[0].Body.(*ast.MappingNode)
	node, isString := mapping.Values[0].Value.(*ast.StringNode)
	if !isString {
		t.Fatalf("the fixed placeholder is %T, want *ast.StringNode", mapping.Values[0].Value)
	}
	node.Value = written

	return newSource("fuzz.yaml", []byte(document)), node
}
