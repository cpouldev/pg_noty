package config

import (
	"fmt"
	"slices"
	"strings"
)

// This file is what a rendered diagnostic block contains, and nothing else. It knows positions and
// text, and no rule at all, which is why a new rule cannot change the layout the corpus froze. Where
// on the page each piece of a block sits is snippet.go's answer.
//
// It also holds no source and turns no bytes into text of its own: the only text it can quote is
// what the redactor hands it, already stripped of secrets (ADR-5, skill Pattern 11). Tests enforce
// that by scanning this file and by pinning which functions of the package may be handed the bytes
// at all (TestNoRenderedOutputPathBypassesTheRedactor).
//
// A diagnostic's own Msg and Hint are the *other* text this file writes, and they are not quoted
// from the redacted lines -- parse.go reuses the parser's wording, which the parser composes out of
// the document. So every one of them goes through the same lines' contained(), and
// TestEveryDiagnosticTextRenderedIsContained asserts that structurally rather than by taste
// (diagnostictext.go).

// The one label the convention froze. A warning is marked as one; errors carry no label, because
// the convention's own block carries none (AC #23 asks only that a warning be marked).
const (
	warningLabel = "warning: "
	errorLabel   = ""
)

// Render returns the diagnostics as positioned, human-readable text: for each, a file:line:col
// header, up to two lines of context, the offending line marked, a caret under the offending
// token and any hint two columns further in.
//
// src is the configuration text the diagnostics were produced from. It is read only to quote it,
// and every schema-declared secret in it is replaced before any of it is quoted, so a rendered
// diagnostic cannot carry a password or a signing secret. Output carries no colour and no
// decoration beyond the above, so it is byte-stable for golden comparison.
func (e Errors) Render(src []byte) string {
	return renderDiagnostics(e, errorLabel, src)
}

// Render returns the warnings in the same positioned layout as Errors.Render, each marked (AC #23).
func (w Warnings) Render(src []byte) string {
	return renderDiagnostics(asErrors(w), warningLabel, src)
}

// renderDiagnostics is the one place rendered text is assembled, and therefore the one place that
// obtains quotable source lines. Blocks are separated by a blank line and each ends in a newline,
// so the whole output is a text file rather than a fragment.
func renderDiagnostics(diags Errors, label string, src []byte) string {
	if len(diags) == 0 {
		return ""
	}

	// The only source text this file can reach, and it is already redacted. A run of
	// positionless diagnostics quotes nothing -- a stage-A failure has no bytes to quote
	// either -- so it reads no source at all.
	var lines quotableLines
	if slices.ContainsFunc(diags, carriesAPosition) {
		lines = redactedLines(src)
	}

	blocks := make([]string, 0, len(diags))
	for _, diag := range diags {
		blocks = append(blocks, renderBlock(diag, label, lines))
	}
	return strings.Join(blocks, "\n")
}

// carriesAPosition reports whether a diagnostic points at a token rather than at a file.
func carriesAPosition(diag Error) bool {
	return diag.Line >= firstLine
}

func renderBlock(diag Error, label string, lines quotableLines) string {
	if !carriesAPosition(diag) {
		return renderWithoutPosition(diag, label, lines)
	}
	return renderWithPosition(diag, label, lines)
}

// renderWithoutPosition renders a diagnostic about a file rather than a token: one that could not
// be read, or that holds no configuration at all. There is nothing to quote and nothing to point
// at, so the message takes the caret line's place and the hint keeps its offset from it.
func renderWithoutPosition(diag Error, label string, lines quotableLines) string {
	var block strings.Builder

	block.WriteString(diag.File + "\n")
	block.WriteString(label + lines.contained(diag.Msg) + "\n")
	writeHint(&block, lines.contained(diag.Hint), firstColumn+hintIndent)
	return block.String()
}

func renderWithPosition(diag Error, label string, lines quotableLines) string {
	snippet := newSnippetBlock(diag.Line)

	// A column below one is not a column at all, and points at the start of the line rather
	// than into the gutter (Note 4). The header keeps this source column; the caret carries
	// it through any length-changing redaction on the quoted line.
	column := max(diag.Col, firstColumn)

	var block strings.Builder
	block.WriteString(fmt.Sprintf("%s:%d:%d\n", diag.File, diag.Line, column))
	for number := snippet.first; number <= diag.Line; number++ {
		block.WriteString(snippet.quote(number, lineAt(lines.text, number), number == diag.Line))
	}

	caret := snippet.caretColumn(lines.column(diag.Line, column))
	block.WriteString(caretAt(caret, label+lines.contained(diag.Msg)) + "\n")
	writeHint(&block, lines.contained(diag.Hint), caret+hintIndent)
	return block.String()
}

func writeHint(block *strings.Builder, hint string, column int) {
	if hint == "" {
		return
	}
	block.WriteString(indentTo(column) + hint + "\n")
}
