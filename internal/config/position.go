package config

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// tabColumns is this package's tab policy, stated once: a tab occupies one column.
//
// Skill Pattern 2 (position safety) requires the policy to be chosen explicitly and
// applied consistently rather than inherited from the parser, which gives a tab no
// width at all. One column is the policy the rendering convention needs, because it
// renders a tab as a single space in a snippet; counting it as one column here is
// what keeps a caret under the character it points at.
const tabColumns = 1

// firstColumn and noColumn are the two ends of what a column can be: columns are counted
// from one, so zero is outside the range and cannot be mistaken for a position. Every
// "there is nothing to point at" answer in this file is noColumn -- a diagnostic about a
// file rather than a token, and a reported column that is not a column at all.
const (
	firstColumn = 1
	noColumn    = 0
)

// yamlPathRoot is the prefix the parser puts on every node path. Diagnostics carry
// the path without it, so a locator reads listeners[0].payload.columns.
const yamlPathRoot = "$"

// sourcePos is the only implementation of Positioned.
//
// It holds what a column is derived *from* -- the raw line and the column the parser
// reported -- rather than a column. There is therefore no field a later stage could fill
// with arithmetic of its own: every Col() answer is runeColumn's, which is what makes the
// derivation a single choke point structurally rather than by convention.
type sourcePos struct {
	file string
	line int
	path string

	// lineText is the raw source line, and reportedColumn the parser's column within
	// it. Both are zero-valued for a positionless diagnostic, which line == 0 names.
	lineText       string
	reportedColumn int
}

func (p sourcePos) File() string { return p.file }
func (p sourcePos) Line() int    { return p.line }
func (p sourcePos) Path() string { return p.path }

// Col derives the reported column. A diagnostic with no line has no column either --
// a file that could not be read has nothing to point at.
func (p sourcePos) Col() int {
	if p.line == 0 {
		return noColumn
	}
	return runeColumn(p.lineText, p.reportedColumn)
}

// positionOf turns a parsed node into a reported position. A nil node yields a
// positionless result rather than a panic.
func positionOf(src *source, node ast.Node) Positioned {
	if node == nil {
		return sourcePos{file: src.Name()}
	}
	return tokenPosition(src, node.GetToken(), pathOf(node))
}

// tokenPosition reports where a token starts. It exists alongside positionOf because
// the parser reports a syntax error as a token without a node; every way of anchoring a
// diagnostic -- a node, a mapping's first key, a bare token -- converges here, and a
// sourcePos exists nowhere else in the package outside this file.
func tokenPosition(src *source, at *token.Token, path string) Positioned {
	if at == nil || at.Position == nil {
		return sourcePos{file: src.Name(), path: path}
	}

	line := at.Position.Line
	return sourcePos{
		file:           src.Name(),
		line:           line,
		path:           path,
		lineText:       src.Line(line),
		reportedColumn: parserReportedColumn(at),
	}
}

// parserReportedColumn is the package's sole read of the library column. Tests
// measuring goccy's accounting call this choke point too, so no second selector
// can grow a competing position derivation.
func parserReportedColumn(at *token.Token) int {
	if at == nil || at.Position == nil {
		return noColumn
	}
	return at.Position.Column
}

// runeColumn reports the 1-based rune column at which a token begins on its raw
// source line.
//
// CONFORMANCE with skill Pattern 2 (position safety), which prescribes re-deriving the column
// from the raw line with an explicit tab policy. Finding V3 measured why the parser's own
// coordinates cannot be reported directly: token.Position.Column advances by zero for a
// tab, so a tab before the token undercounts, and token.Position.Offset drifts by one for
// every preceding comment line and every preceding block scalar (Implementation Note 6).
// Offset is therefore read nowhere in this package, which a source-scanning test enforces.
//
// One mechanism of Pattern 2 is substituted, which the task's Definition of Done requires
// stating here. Pattern 2 prescribes scanning the raw line up to the *token text* taken
// from the AST; this scans up to the *reported column* instead. The token text cannot
// serve as the stop condition: it is ambiguous whenever it occurs more than once on its
// line -- `columns: [id, id]` has two of them and only the second is at fault -- and an
// *ast.NullNode, which is how an omitted value is anchored, has no text at all. The
// substitution's cost is that the parser's zero-width tab becomes an input this function
// depends on rather than a value it merely distrusts, so TestGoccyGivesATabZeroWidth pins
// that accounting by name in every YAML style: an upgrade that gives a tab a width fails
// there first, instead of surfacing as an unexplained wrong column.
//
// The derivation therefore reproduces the parser's accounting against the raw line
// instead of adjusting the number it produced: it walks the line's runes tracking both
// what the parser would have counted and what a rendered snippet needs, and answers with
// the last rune that shares the reported column. Taking the last one steps over the run
// of tabs the parser collapsed onto that column, which is exactly the undercount.
//
// One class is knowingly one column out, because the line alone cannot decide it: when
// the token *is* a tab -- which is how the parser reports a tab used as indentation --
// stepping over the run of tabs steps over the token itself, so the answer is the
// character after it. Telling that case apart would mean branching on the text of an
// invalid token, so it is recorded and asserted instead (Implementation Note 10).
func runeColumn(line string, reportedColumn int) int {
	// A column below one is not a column, so no rune can carry it and the scan has
	// nothing to look for. Falling through to the clamp below would treat it as merely
	// past the line's end and answer one past the last rune -- a legal-looking column
	// offered for a position the parser never reported.
	if reportedColumn < firstColumn {
		return noColumn
	}

	parserColumn, renderedColumn := firstColumn, firstColumn
	start := noColumn

	for _, char := range line {
		if parserColumn == reportedColumn {
			start = renderedColumn
		}

		if char == '\t' {
			// The parser gives a tab no width, so its column does not advance here.
			renderedColumn += tabColumns
			continue
		}
		parserColumn++
		renderedColumn++
	}

	if start == noColumn {
		// No rune of the line carries the reported column, which happens when the
		// token sits past the line's end: a key with its value omitted is a null node
		// anchored there (Implementation Note 7). One past the last rendered rune is
		// where its caret belongs, and is the only column a line with no runes at all
		// can offer.
		return renderedColumn
	}
	return start
}

// pathOf is the node locator a diagnostic carries: the parser's node path without
// its root prefix. The document root itself has no locator.
func pathOf(node ast.Node) string {
	path := node.GetPath()
	if path == yamlPathRoot {
		return ""
	}
	return strings.TrimPrefix(path, yamlPathRoot+".")
}
