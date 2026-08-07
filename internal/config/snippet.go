package config

import (
	"fmt"
	"strconv"
	"strings"
)

// This file is the geometry of a snippet: how wide the gutter is, which column the caret lands in,
// and how one quoted line is laid out. render.go decides what a diagnostic block contains; this
// decides where on the page each piece of it sits.
//
// The two were one file until render.go reached 199 lines of its 200-line budget and that saturation
// became the stated reason a known caret defect was left unfixed (Implementation Note 18). The split
// is what gives the next step room to take that fix, which is worth more than the fix itself was.
//
// It holds no source of its own and turns no bytes into text: the only text it lays out is what the
// redactor already handed the renderer (ADR-5, skill Pattern 11).

// Everything the convention froze about a snippet's decoration. The two markers are the same width
// on purpose -- a caret column computed against one has to be right on the other, which
// TestBothSnippetMarkersAreTheSameWidth pins.
const (
	offendingMarker = ">  "
	contextMarker   = "   "
	gutterBar       = " | "

	// maxContextLines is the convention's cap: two leading lines, never a trailing one,
	// because a diagnostic is about what precedes its token rather than what follows it.
	maxContextLines = 2
	// hintIndent is how much further in than the caret a hint line starts.
	hintIndent = 2
	// firstLine is where line numbering starts, as firstColumn is for columns.
	firstLine = 1
)

// tabRendering is a tab as a snippet shows it. It is derived from the column policy rather than
// written beside it: tabColumns says a tab occupies one column, so rendering it as anything but
// that many spaces would move every rune after it away from the column a caret was computed for.
var tabRendering = strings.Repeat(" ", tabColumns)

// snippetBlock is the geometry every line of one diagnostic's snippet shares. The gutter width,
// the caret column and the hint column are all computed from prefixWidth, so they cannot
// disagree with each other.
type snippetBlock struct {
	first       int
	numberWidth int
	prefixWidth int
}

func newSnippetBlock(offending int) snippetBlock {
	// The widest number in the block is the offending line: the block is an ascending run
	// of line numbers ending there, and a number's digit count never shrinks as it grows.
	width := len(strconv.Itoa(offending))

	return snippetBlock{
		first:       max(firstLine, offending-maxContextLines),
		numberWidth: width,
		prefixWidth: len(offendingMarker) + width + len(gutterBar),
	}
}

// caretColumn is where the caret goes: the prefix width plus the diagnostic's own source column,
// which its caller has already clamped for the header as well.
func (b snippetBlock) caretColumn(sourceColumn int) int {
	return b.prefixWidth + sourceColumn
}

// quote renders one snippet line: the marker, the right-aligned number, the bar, and the source
// line with every tab rendered as the single space the column policy counts it as.
//
// Trailing spaces are trimmed rather than quoted. They are invisible in a snippet either way, and
// a golden carrying them would be silently broken by any editor or CI check that strips them.
func (b snippetBlock) quote(number int, text string, offending bool) string {
	marker := contextMarker
	if offending {
		marker = offendingMarker
	}

	quoted := marker + fmt.Sprintf("%*d", b.numberWidth, number) + gutterBar +
		strings.ReplaceAll(text, "\t", tabRendering)
	return strings.TrimRight(quoted, " ") + "\n"
}

// lineAt is the quotable text of a 1-based line, or nothing when the diagnostic points past the
// text it is rendered against -- which a caller arranges by passing bytes other than the ones the
// diagnostics came from, and a positionless run by needing no source at all
// (TestALineNumberPastTheSourceQuotesNothing).
func lineAt(lines []string, number int) string {
	if number < firstLine || number > len(lines) {
		return ""
	}
	return lines[number-1]
}

// caretAt points at a rendered column and states the diagnostic beside the caret. The space after
// the caret is trimmed rather than branched on, by the same rule that trims a quoted line's own
// trailing spaces: no rendered line ends in whitespace this package added.
func caretAt(column int, text string) string {
	return strings.TrimRight(indentTo(column)+"^ "+text, " ")
}

// indentTo is the run of spaces that puts the next character at the given 1-based column.
func indentTo(column int) string {
	return strings.Repeat(" ", column-1)
}
