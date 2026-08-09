package config

import (
	goast "go/ast"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// Stage D's own file: the readers every other stage-D test runs through, and the conformance
// comment the recursion's call site is required to carry.
//
// The questions the stage is assembled from are asserted beside the files that own them, as
// the production files are split: envreference_test.go the grammar, valueposition_test.go
// where it may be applied, opacity_test.go what a substituted value is afterwards,
// stagedpolicy_test.go what a run does with what it found.
//
// SC-7's first half -- that ast.Walk is called nowhere in this package -- is asserted
// package-wide by TestParseWalksEachDocumentRatherThanTheFile (parse_test.go) and is not
// repeated here; this file asserts the conformance comment that scan is paired with.

const interpolationFixture = "listeners.yaml"

// stageD runs interpolation over text and reports the root it substituted into, the
// originals it recorded and the diagnostics it raised.
//
// It fails the test when the fixture does not reach stage D at all, so a row whose YAML is
// malformed says so instead of quietly asserting nothing.
func stageD(t *testing.T, text string, vars map[string]string) (*source, ast.Node, originalTexts, Errors) {
	t.Helper()

	src := newSource(interpolationFixture, []byte(text))
	root, diags := parseDocument(src)
	if len(diags) != 0 {
		t.Fatalf("the fixture does not reach stage D: %+v\nsource:\n%s", diags, text)
	}

	originals, faults := interpolate(src, root, MapEnv(vars))
	return src, root, originals, faults
}

// textIn is what the library decodes the node at path to.
//
// The oracle is the library's own decoder rather than this package's node access, which is
// what makes "the substituted value is opaque" a claim about what a consumer receives: a
// re-parse or a re-quote anywhere between the substitution and the decode shows up as a
// value that is no longer byte-identical.
func textIn(t *testing.T, root ast.Node, path string) string {
	t.Helper()

	var decoded string
	if err := yaml.NodeToValue(nodeIn(t, root, path), &decoded); err != nil {
		t.Fatalf("decoding %q failed: %v", path, err)
	}
	return decoded
}

// messagesOf is each diagnostic's message, in the order the collection holds them.
func messagesOf(diags Errors) []string {
	messages := make([]string, len(diags))
	for i, diag := range diags {
		messages[i] = diag.Msg
	}
	return messages
}

// declarationOf is the named function's whole declaration, failing the test when the file
// declares no such function so that a rename cannot make an assertion pass vacuously.
func declarationOf(t *testing.T, file *goast.File, function string) *goast.FuncDecl {
	t.Helper()

	for _, decl := range file.Decls {
		if declared, isFunction := decl.(*goast.FuncDecl); isFunction && declared.Name.Name == function {
			return declared
		}
	}

	t.Fatalf("no function %s is declared here", function)
	return nil
}
