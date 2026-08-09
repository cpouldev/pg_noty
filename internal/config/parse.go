package config

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// duplicateKeyMessage is how the parser words the duplicate mapping key it detects while
// parsing (V6, measured in Implementation Note 5, which is the one home for the exact
// wording). Recognising that wording is what maps the condition onto rule R42, because
// the library exposes no typed error for it.
//
// The whole message is matched and anchored at both ends rather than searched for a
// phrase, so a different syntax error that happens to quote those words -- out of the
// user's own configuration text, which the parser echoes -- cannot be misattributed to
// R42 and corrupt the rule inventory. A named pin test asserts that the parser's wording
// still matches, so a dependency upgrade that rewords it fails that test rather than
// silently demoting R42 to an anonymous syntax error.
var duplicateKeyMessage = regexp.MustCompile(`^mapping key .* already defined at \[\d+:\d+\]$`)

// unrecognisedParseFailureMessage is what a parse failure of a shape this package cannot read is
// reported as. It names the condition in this package's own words and quotes nothing, because the
// text it replaces could hold any part of the document.
const unrecognisedParseFailureMessage = "configuration file could not be parsed"

// emptyDocumentMessage covers both shapes of a file that configures nothing, which
// are one condition rather than two: a file with no content, and a file whose only
// content is comments.
const emptyDocumentMessage = "configuration file is empty or contains only comments"

// parseDocument runs stage B (parse) and stage C (document). Both are fatal-single:
// a document that did not parse, or that does not hold exactly one mapping, cannot
// carry a second meaningful diagnostic, so the first one ends the run and no root is
// returned.
func parseDocument(src *source) (ast.Node, Errors) {
	// Stage B. Comments are parsed rather than discarded so that a comment-only file
	// is distinguishable from an empty one below.
	file, err := parser.ParseBytes(src.CloneBytes(), parser.ParseComments)
	if err != nil {
		return nil, Errors{syntaxDiagnostic(src, err)}
	}

	return documentRoot(src, file)
}

// syntaxDiagnostic turns the parser's own error into one positioned diagnostic.
func syntaxDiagnostic(src *source, err error) Error {
	rule, message, at := classifyParseError(err)
	if at == nil {
		return NewFileError(rule, src.Name(), message)
	}
	return NewError(rule, tokenPosition(src, at, ""), message)
}

// classifyParseError extracts the rule, the message and the offending token from a
// parse failure. The parser's message is reused verbatim because it already states
// the condition precisely -- for a duplicate key it names the line of the first
// occurrence, which is what the caller has to be told.
func classifyParseError(err error) (RuleID, string, *token.Token) {
	var syntax *yaml.SyntaxError
	if !errors.As(err, &syntax) {
		// An error shape this package does not recognise still has to be reported rather than
		// dropped, even though it cannot be positioned -- and it is reported without its own text.
		//
		// This arm used to return err.Error(). A *yaml.SyntaxError's Error() is a pretty-printed
		// block of the source around the failure, measured; an unrecognised shape's is by definition
		// unmeasured, and a diagnostic carrying one is rendered without a position, which is exactly
		// the case redactedLines reads no source for -- so nothing downstream could withhold a byte
		// of it. A default that hands unmeasured text to the page is fail-open in the one component
		// whose contract is to over-redact.
		//
		// Nothing goccy v1.19.2 returns from ParseBytes reaches here, which
		// TestEveryParseFailureThisLibraryProducesIsASyntaxError asserts over the invalid corpus;
		// the refusal itself is asserted by
		// TestAnUnrecognisedParseFailureIsReportedWithoutItsText.
		return RuleSyntax, unrecognisedParseFailureMessage, nil
	}

	if duplicateKeyMessage.MatchString(syntax.Message) {
		return R42, syntax.Message, syntax.Token
	}
	return RuleSyntax, syntax.Message, syntax.Token
}

// documentRoot runs stage C: the three document-level conditions the parser reports
// as success. It iterates file.Docs per V1 and skill Pattern 1 (walk correctly) --
// *ast.File does not implement ast.Node, so the library's generic walk cannot be
// handed the file itself, and per-document traversal is the only correct form.
func documentRoot(src *source, file *ast.File) (ast.Node, Errors) {
	bodies := contentBodies(file)

	if len(bodies) == 0 {
		return nil, Errors{NewFileError(RuleDocument, src.Name(), emptyDocumentMessage)}
	}
	if len(bodies) > 1 {
		message := fmt.Sprintf("configuration file must contain exactly one YAML document, found %d", len(bodies))
		// Anchored on the second document, which is the first one that should not be
		// there -- through its first key rather than its own token, which is the colon
		// of that key (ADR-6).
		return nil, Errors{NewError(RuleDocument, positionOfMapping(src, bodies[1]), message)}
	}

	root := bodies[0]
	if root.Type() != ast.MappingType {
		// The parser's own name for the shape it found, so the diagnostic speaks YAML's
		// vocabulary rather than one invented here (Implementation Note 9, which records
		// what that means for a renderer's goldens).
		message := fmt.Sprintf("configuration root must be a mapping, found %s", root.Type().YAMLName())
		return nil, Errors{NewError(RuleDocument, positionOf(src, root), message)}
	}
	return root, nil
}

// contentBodies returns the documents that carry configuration. A document with no
// body -- an empty file, or a trailing `---` (Implementation Note 4) -- and a comment-only
// document, whose body reports ast.CommentType rather than being nil (Implementation
// Note 1), both configure nothing, so neither counts towards the one-document rule.
func contentBodies(file *ast.File) []ast.Node {
	var bodies []ast.Node

	for i := range file.Docs {
		// Body is read rather than GetToken: the library dereferences the body
		// unconditionally, so taking a bodiless document's token panics
		// (Implementation Note 2).
		body := file.Docs[i].Body
		if body == nil || body.Type() == ast.CommentType {
			continue
		}
		bodies = append(bodies, body)
	}

	return bodies
}
