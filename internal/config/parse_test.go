package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

func TestDuplicateKeyDiagnosticNamesTheFirstOccurrencesLine(t *testing.T) {
	src := newSource("listeners.yaml", []byte("a: 1\nb:\n  c: 1\n  c: 2\n"))

	_, diags := parseDocument(src)

	if len(diags) != 1 {
		t.Fatalf("parseDocument() returned %d diagnostics, want exactly 1", len(diags))
	}
	if diags[0].Line != 4 {
		t.Errorf("diagnostic anchored on line %d, want the second occurrence on line 4", diags[0].Line)
	}
	if !strings.Contains(diags[0].Msg, "[3:3]") {
		t.Errorf("Msg = %q, want it to name the first occurrence at [3:3]", diags[0].Msg)
	}
}

// TestParseDocumentAcceptsAMappingRoot covers the inputs stage C must let through,
// including the near-misses of the guard that discards a comment body: a comment header
// followed by content is the shape of the product's own documented configuration, and a
// guard that started matching it would reject every commented file with "empty or
// contains only comments".
func TestParseDocumentAcceptsAMappingRoot(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{name: "single key", src: "version: 1\n"},
		{name: "trailing document separator starts no second document", src: "version: 1\n---\n"},
		{name: "leading document separator", src: "---\nversion: 1\n"},
		{name: "trailing comment", src: "version: 1\n# done\n"},
		{name: "comment header before content", src: "# pg_noty configuration\nversion: 1\n"},
		{name: "comment header separated from content by a blank line", src: "# pg_noty configuration\n\nversion: 1\n"},
		{name: "comment header before a document separator", src: "# pg_noty configuration\n---\nversion: 1\n"},
		{name: "explicitly empty listeners list", src: "version: 1\nlisteners: []\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := newSource("listeners.yaml", []byte(tc.src))

			root, diags := parseDocument(src)

			if len(diags) != 0 {
				t.Fatalf("parseDocument() returned %d diagnostics, want none: %+v", len(diags), diags)
			}
			if root == nil {
				t.Fatal("parseDocument() returned no root node")
			}
			if root.Type() != ast.MappingType {
				t.Errorf("root node type = %v, want %v", root.Type(), ast.MappingType)
			}
		})
	}
}

// TestSyntaxDiagnosticReportsAnErrorShapeItCannotPosition covers the fallback for a
// parse failure that is not a *yaml.SyntaxError: an unrecognised shape still has to
// be reported rather than dropped, and it can only be reported against the file.
//
// It is reported **without its own text**, which is the clause that changed and why: a
// positionless diagnostic renders on a path that reads no source, so nothing downstream can
// withhold a byte of an unmeasured message (diagnostictext.go). The wording itself is asserted by
// TestAnUnrecognisedParseFailureIsReportedWithoutItsText.
func TestSyntaxDiagnosticReportsAnErrorShapeItCannotPosition(t *testing.T) {
	src := newSource("listeners.yaml", []byte("version: 1\n"))
	unrecognised := errors.New("the parser failed in a shape this package does not know")

	diag := syntaxDiagnostic(src, unrecognised)

	if diag.Rule != RuleSyntax {
		t.Errorf("Rule = %q, want %q", diag.Rule, RuleSyntax)
	}
	if diag.Msg != unrecognisedParseFailureMessage {
		t.Errorf("Msg = %q, want %q", diag.Msg, unrecognisedParseFailureMessage)
	}
	if strings.Contains(diag.Msg, unrecognised.Error()) {
		t.Errorf("Msg = %q, which quotes the unrecognised error's own text", diag.Msg)
	}
	if diag.Line != 0 || diag.Col != 0 {
		t.Errorf("diagnostic carries position %d:%d, want none: there is no token to anchor on", diag.Line, diag.Col)
	}
	if diag.File != "listeners.yaml" {
		t.Errorf("File = %q, want %q", diag.File, "listeners.yaml")
	}
}
