package config

import "testing"

type parseCondition struct {
	name     string
	src      string
	wantRule RuleID
	wantLine int
	wantCol  int
	wantMsg  string
}

var parseConditions = []parseCondition{
	{
		// A file-level condition has no token to anchor on, so it has no position. The
		// message is literal so an expectation cannot drift with the production constant.
		name:     "empty file",
		wantRule: RuleDocument,
		wantMsg:  "configuration file is empty or contains only comments",
	},
	{
		name:     "whitespace only file",
		src:      "   \n\n",
		wantRule: RuleDocument,
		wantMsg:  "configuration file is empty or contains only comments",
	},
	{
		name:     "comment only file",
		src:      "# nothing but a comment\n",
		wantRule: RuleDocument,
		wantMsg:  "configuration file is empty or contains only comments",
	},
	{
		// Line 1 is `- name: one`: the sequence's own token is the `-` at rune 1.
		name:     "sequence root",
		src:      "- name: one\n- name: two\n",
		wantRule: RuleDocument,
		wantLine: 1,
		wantCol:  1,
		wantMsg:  "configuration root must be a mapping, found sequence",
	},
	{
		// Line 1 is `just a string`: the scalar begins at rune 1.
		name:     "scalar root",
		src:      "just a string\n",
		wantRule: RuleDocument,
		wantLine: 1,
		wantCol:  1,
		wantMsg:  "configuration root must be a mapping, found string",
	},
	{
		// The second document's first key begins at rune 1; its mapping token is the colon.
		name:     "two documents",
		src:      "version: 1\n---\nversion: 1\n",
		wantRule: RuleDocument,
		wantLine: 3,
		wantCol:  1,
		wantMsg:  "configuration file must contain exactly one YAML document, found 2",
	},
	{
		// Line 3 is `  schema: other`: two spaces put the repeated key at rune 3.
		name:     "duplicate mapping key",
		src:      "database:\n  schema: noty\n  schema: other\n",
		wantRule: R42,
		wantLine: 3,
		wantCol:  3,
		wantMsg:  `mapping key "schema" already defined at [2:3]`,
	},
	{
		// `listeners: ` is 11 runes, so the unterminated sequence starts at rune 12.
		name:     "syntax error",
		src:      "listeners: [one, two\n",
		wantRule: RuleSyntax,
		wantLine: 1,
		wantCol:  12,
		wantMsg:  "sequence end token ']' not found",
	},
	{
		// Goccy reports both the indentation tab and the following `s` at column 1. The
		// package derives the last matching rune, so the visible caret is rune 2.
		name:     "tab indentation",
		src:      "database:\n\tschema: noty\n",
		wantRule: RuleSyntax,
		wantLine: 2,
		wantCol:  2,
		wantMsg:  "found character '\t' that cannot start any token",
	},
}

// TestParseDocumentReportsOneDiagnosticPerStructuralCondition asserts the whole
// diagnostic each condition produces, including its mechanically counted caret column.
func TestParseDocumentReportsOneDiagnosticPerStructuralCondition(t *testing.T) {
	for _, tc := range parseConditions {
		t.Run(tc.name, func(t *testing.T) {
			src := newSource("listeners.yaml", []byte(tc.src))
			root, diags := parseDocument(src)
			if len(diags) != 1 {
				t.Fatalf("parseDocument() returned %d diagnostics, want exactly 1: %+v", len(diags), diags)
			}
			if root != nil {
				t.Errorf("parseDocument() returned a root node alongside a diagnostic")
			}
			got := diags[0]
			if got.Rule != tc.wantRule {
				t.Errorf("Rule = %q, want %q", got.Rule, tc.wantRule)
			}
			if got.Line != tc.wantLine || got.Col != tc.wantCol {
				t.Errorf("position = %d:%d, want %d:%d (line %q)",
					got.Line, got.Col, tc.wantLine, tc.wantCol, src.Line(got.Line))
			}
			if got.Msg != tc.wantMsg {
				t.Errorf("Msg = %q, want %q", got.Msg, tc.wantMsg)
			}
			if got.File != "listeners.yaml" {
				t.Errorf("File = %q, want listeners.yaml", got.File)
			}
		})
	}
}
