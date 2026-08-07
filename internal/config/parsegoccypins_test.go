package config

import (
	"errors"
	"slices"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// TestGoccyReportsDuplicateKeysAtParseTime pins V6 and stage B's fatal-single policy.
func TestGoccyReportsDuplicateKeysAtParseTime(t *testing.T) {
	_, err := parser.ParseBytes([]byte("b:\n  c: 1\n  c: 2\n"), parser.ParseComments)
	if err == nil {
		t.Fatal("parser.ParseBytes accepted a duplicate mapping key")
	}
	var syntax *yaml.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("parse error is %T, want *yaml.SyntaxError", err)
	}
	if want := `mapping key "c" already defined at [2:3]`; syntax.Message != want {
		t.Errorf("SyntaxError.Message = %q, want %q", syntax.Message, want)
	}
	if !duplicateKeyMessage.MatchString(syntax.Message) {
		t.Errorf("the R42 pattern no longer matches %q", syntax.Message)
	}
}

func TestOnlyTheParsersOwnDuplicateKeyWordingBecomesR42(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		wantRule RuleID
	}{
		{"the parser's wording", `mapping key "schema" already defined at [2:3]`, R42},
		{"a quoted user phrase", `could not find expected ':' in "already defined at the top"`, RuleSyntax},
		{"no parser position suffix", `mapping key "schema" already defined at the top`, RuleSyntax},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rule, message, _ := classifyParseError(&yaml.SyntaxError{Message: tc.message})
			if rule != tc.wantRule {
				t.Errorf("classifyParseError(%q) = %q, want %q", tc.message, rule, tc.wantRule)
			}
			if message != tc.message {
				t.Errorf("message = %q, want %q", message, tc.message)
			}
		})
	}
}

// TestGoccyGivesAMappingTheColonOfItsFirstEntry pins the measurement behind ADR-6.
func TestGoccyGivesAMappingTheColonOfItsFirstEntry(t *testing.T) {
	file, err := parser.ParseBytes([]byte("version: 1\n---\nversion: 1\n"), parser.ParseComments)
	if err != nil {
		t.Fatalf("parser.ParseBytes returned %v, want nil", err)
	}
	got := file.Docs[1].Body.GetToken()
	if got.Value != ":" {
		t.Errorf("mapping token is %q, want the first entry's colon", got.Value)
	}
	if got.Position.Line != 3 || parserReportedColumn(got) != 8 {
		t.Errorf("mapping token at [%d:%d], want [3:8]", got.Position.Line, parserReportedColumn(got))
	}
}

// TestGoccyDocumentLevelBehaviourIsPinned pins V7 and the nil/comment body shapes.
func TestGoccyDocumentLevelBehaviourIsPinned(t *testing.T) {
	const noBody = ast.UnknownNodeType
	tests := []struct {
		name       string
		src        string
		wantBodies []ast.NodeType
	}{
		{"empty file", "", []ast.NodeType{noBody}},
		{"comment only", "# just a comment\n", []ast.NodeType{ast.CommentType}},
		{"comment then content", "# header\nversion: 1\n", []ast.NodeType{ast.MappingType}},
		{"two documents", "a: 1\n---\nb: 2\n", []ast.NodeType{ast.MappingType, ast.MappingType}},
		{"sequence root", "- one\n", []ast.NodeType{ast.SequenceType}},
		{"trailing separator", "a: 1\n---\n", []ast.NodeType{ast.MappingType, noBody}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseBytes([]byte(tc.src), parser.ParseComments)
			if err != nil {
				t.Fatalf("parser.ParseBytes(%q): %v", tc.src, err)
			}
			got := make([]ast.NodeType, len(file.Docs))
			for i, doc := range file.Docs {
				got[i] = noBody
				if doc.Body != nil {
					got[i] = doc.Body.Type()
				}
			}
			if !slices.Equal(got, tc.wantBodies) {
				t.Errorf("document bodies = %v, want %v", got, tc.wantBodies)
			}
		})
	}
}

func TestGoccyPanicsWhenTakingTheTokenOfABodilessDocument(t *testing.T) {
	file, err := parser.ParseBytes(nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parser.ParseBytes(empty) returned %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Error("DocumentNode.GetToken no longer panics on a nil Body")
		}
	}()
	_ = file.Docs[0].GetToken()
}

func TestGoccyLeavesUndefinedAliasesForALaterStage(t *testing.T) {
	file, err := parser.ParseBytes([]byte("a: *undefined\n"), parser.ParseComments)
	if err != nil {
		t.Fatalf("undefined aliases are stage E's problem, not stage B's: %v", err)
	}
	if file.Docs[0].Body == nil {
		t.Fatal("expected a mapping body")
	}
}
