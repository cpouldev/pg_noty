package config

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// This file covers redact.go: the choke point, the ordering it enforces and the boundaries
// that make containment structural rather than a habit. What is redacted where is
// sensitivepaths_test.go and sensitivekeys_test.go; that no path escapes the redactor at all
// is here.

// The listener's `table` and `operations` are written *last* rather than beside its name,
// because two of this file's assertions count from line 3 and line 8. Stage F requires both keys
// of every listener, and adding them above the destination would have moved the two lines those
// assertions are derived from.
//
// secretBearingSource holds both halves of D3's deliberate asymmetry in one document, so a
// redactor that blanked by value rather than by path could not pass: the database URL's
// password must go, and the destination URL must survive intact even though it too is a
// URL and even though its scheme is the one AC #20 rejects.
const secretBearingSource = `version: 1
database:
  url: postgres://noty:PGNOTY-SENTINEL-db@db.internal:5432/noty
  schema: noty
listeners:
- name: order_paid
  destination:
    url: ftp://host/x
    signing:
      secrets:
      - PGNOTY-SENTINEL-literal
      - ${SIGNING_SECRET}
  table: public.orders
  operations: [insert]
`

// TestRenderingLeavesTheSourceUnchanged is ADR-5's immutability half: redaction happens on
// a copy, so neither the caller's bytes nor a source built from them can be altered by
// rendering, and a second render of the same bytes cannot differ from the first.
func TestRenderingLeavesTheSourceUnchanged(t *testing.T) {
	original := []byte(secretBearingSource)
	untouched := bytes.Clone(original)
	src := newSource("listeners.yaml", original)
	linesBefore := slices.Clone(src.lines)

	first := renderEveryLineOf(secretBearingSource)

	if !bytes.Equal(original, untouched) {
		t.Errorf("rendering wrote through the caller's byte slice:\ngot  %q\nwant %q", original, untouched)
	}
	if !bytes.Equal(src.CloneBytes(), untouched) {
		t.Error("source.bytes changed while rendering")
	}
	if !slices.Equal(src.lines, linesBefore) {
		t.Error("source.lines changed while rendering")
	}
	if again := renderEveryLineOf(secretBearingSource); again != first {
		t.Error("rendering the same bytes twice produced different output")
	}
}

// TestARenderCopyDoesNotAliasItsSource is the clone inside newRenderCopy, asserted rather than
// assumed. That copy is the whole of what keeps a replacement out of the bytes a renderer quotes,
// and until this existed the clone could be deleted without failing anything -- which left the
// guarantee redact.go calls structural resting on a convention at one edge.
//
// The source above cannot serve: TestRenderingLeavesTheSourceUnchanged builds its own and so cannot
// observe the one redactedLines makes internally. This writes the copy directly instead.
func TestARenderCopyDoesNotAliasItsSource(t *testing.T) {
	text := newSource("listeners.yaml", []byte(secretBearingSource))
	before := slices.Clone(text.lines)

	copied := newRenderCopy(text)
	copied.rewrite(firstLine, redactionPlaceholder)

	if got := copied.line(firstLine); got != redactionPlaceholder {
		t.Fatalf("the copy was not written, so aliasing could not show here: line 1 = %q", got)
	}
	if !slices.Equal(text.lines, before) {
		t.Errorf("writing the render copy wrote the source's own lines:\ngot  %q\nwant %q",
			text.lines, before)
	}
}

// TestNeitherRedactionBranchWritesTheSourcesLines closes the same guarantee at the two entry points
// redactedLines calls, on both sides of D3's branch condition. Each is handed the source it derives
// from and must answer with lines of its own -- the fallback is given that very slice, and the
// path-aware branch is given the source and makes its own copy -- so neither can be the function
// that corrupts the text a later render quotes.
func TestNeitherRedactionBranchWritesTheSourcesLines(t *testing.T) {
	tests := []struct {
		branch string
		redact func(*source, ast.Node) quotableLines
	}{
		{
			branch: "key-scoped fallback",
			redact: func(text *source, _ ast.Node) quotableLines {
				return newQuotableLines(redactBySensitiveKeyName(
					text.lines, text.continuesPhysicalLine))
			},
		},
		{branch: "path-aware", redact: redactDeclaredPaths},
	}

	for _, tc := range tests {
		t.Run(tc.branch, func(t *testing.T) {
			text := newSource("listeners.yaml", []byte(secretBearingSource))
			before := slices.Clone(text.lines)

			root, _ := parseDocument(text)
			redacted := tc.redact(text, root)

			if slices.Equal(redacted.text, before) {
				t.Fatal("the branch replaced nothing, so writing the source could not show here")
			}
			if !slices.Equal(text.lines, before) {
				t.Errorf("the %s branch wrote the source's own lines:\ngot  %q\nwant %q",
					tc.branch, text.lines, before)
			}
		})
	}
}

// TestEveryCarriageReturnSpellingKeepsThePathAwareBranch is what this test became when the defect behind
// it was fixed, and the change of subject is the point.
//
// Step 2 measured that a carriage return outside a CRLF pair was a line break to the parser and content to
// splitLines, so from it onwards the two counted lines differently and a position resolved by the parser
// named a different line than a renderer quoted. Blanking the wrong line is a leak, so such a document was
// sent to the key-scoped fallback. That was the defensive half of a real defect, and the defect has since
// been fixed at its source: splitLines counts exactly the break set the parser counts
// (TestEveryLineBreakTheParserCountsIsCountedHere), so no document diverges and the exclusion was removed
// rather than kept as a belt -- a guard that can no longer fire is false assurance.
//
// So every spelling now keeps the precise branch, which is a strictly better outcome than the fallback it
// used to take: fewer lines blanked, for the same safety. The rows are kept rather than deleted because
// what they now assert is that the fix reaches all three, and a regression in splitLines would send the
// first of them back to the fallback.
//
// The branch's remaining exclusions are asserted below, so this is not a test that only ever expects
// true.
func TestEveryCarriageReturnSpellingKeepsThePathAwareBranch(t *testing.T) {
	documents := map[string]string{
		"a lone carriage return":                    "a: \"first\rsecond\"\nafter: x\n",
		"the carriage return a CRLF line ends in":   "a: \"first\"\r\nafter: x\r\n",
		"no carriage return at all":                 "a: \"first\"\nafter: x\n",
		"a carriage return with no line feed after": "a: 1\rafter: x\n",
	}

	for name, document := range documents {
		t.Run(name, func(t *testing.T) {
			text := newSource("", []byte(document))
			root, diags := parseDocument(text)
			if len(diags) != 0 {
				t.Fatalf("the document does not parse, so it cannot show which branch a parsed one takes: %+v", diags)
			}

			if !pathsAreResolvable(root, diags) {
				t.Error("the path-aware branch was refused for a document whose line counting agrees")
			}

			// The property the branch rests on, asserted directly rather than through the branch: the line
			// the parser puts `after:` on is the line holding its text. This comparison failing *is* the
			// defect the exclusion used to hide.
			_, node := nodeAt(t, document, "$.after")
			parserLine := positionOf(text, node).Line()
			ourLine := slices.IndexFunc(text.lines, func(line string) bool {
				return strings.HasPrefix(line, "after:")
			}) + 1
			if parserLine != ourLine {
				t.Errorf("the parser puts `after:` on line %d and splitLines on line %d; a replacement "+
					"applied to the wrong line is a leak", parserLine, ourLine)
			}
		})
	}
}
