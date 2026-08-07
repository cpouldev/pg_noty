package config

// This file answers one question for D3's path-aware branch: past the line a sensitive value is
// anchored on, how far does that value reach? It is asked only when redactValue could not read the
// value's extent from its own line, which is the state in which D3 prefers over-redaction to a guess.
//
// The fallback branch answers the same question in sensitivekeys.go, and the two must not disagree
// about any input class. So the lexical half of the answer is the fallback's own
// sensitiveContinuation rather than a second reading of the quote and flow grammars.

// blankTheOpenerGapAbove blanks the lines the value's own key opened above the line its text begins
// on -- the opener gap.
//
// A value written below its key is anchored on its own first token, and between that token and the
// key's colon the grammar admits only comments, node properties and blank lines. Every one of them
// belongs to the block the colon opened and to no other value, so the region is blanked whole and
// nothing here reads what is written in it: the bound is the two delimiters around it.
//
// It is called for every value rather than only for the ones that span lines, because a scalar
// written below its key is replaced in place and reaches nowhere -- and can still have a note above
// it. A blank line is skipped, because its own bytes hide nothing and blanking one would put a
// placeholder where the author wrote nothing at all.
func blankTheOpenerGapAbove(copied renderCopy, value sensitiveValue) {
	if value.reachesBackTo < firstLine {
		return
	}

	for number := value.reachesBackTo; number < value.line; number++ {
		written := copied.line(number)
		if isBlankLine(written) {
			continue
		}
		copied.rewrite(number, blankPastIndentation(written))
	}
}

// blankValueReach blanks every line a sensitive value reaches past its anchor.
//
// A container the parser closed for us needs no guessing: its closing line is the extent, and a flow
// container's continuation may sit at any column, so indentation must not be consulted for one.
// Everything else is walked, because a scalar has no closing token.
func blankValueReach(copied renderCopy, text *source, value sensitiveValue) {
	if value.throughLine > value.line {
		blankThroughClosingLine(copied, value.line, value.throughLine)
		return
	}

	// Read the lexical state from the source line, never from the copy: the copy's anchor line has
	// already had this value replaced by the placeholder, whose bytes open nothing.
	written := text.Line(value.line)
	blankIndentedContinuation(copied, value.line,
		leastIndentInsideTheValue(written, value.beginsOnALineOfItsOwn),
		leftOpenAfter(written, value.column))
}

// leastIndentInsideTheValue is the smallest indentation a later line can have and still belong to
// this value.
//
// A value anchored on the line its key is written on begins after that key, so only a line indented
// *past* the key is inside it. A block container is anchored on a line of its own text, so that
// line's own indentation is already inside the value and its sibling entries sit at exactly that
// column: `secrets:` holding three entries reaches all three, where the key-anchored rule stopped at
// the first and rendered the other two.
//
// Reading the anchor line's indentation rather than the column the value's text begins at is the
// fail-closed direction, and costs a `- ` marker's width when a block mapping is written as a
// sequence item: a sibling of the *list* written at the marker's own column is blanked with the
// value. That is the over-redaction D3 prefers, and it is the same width writtenStartOf already
// takes at the other end of the same value.
func leastIndentInsideTheValue(anchor string, beginsOnALineOfItsOwn bool) int {
	indent := indentWidth(anchor)
	if beginsOnALineOfItsOwn {
		return indent
	}
	return indent + 1
}

// blankThroughClosingLine blanks up to and including the closing line the parser reported.
func blankThroughClosingLine(copied renderCopy, anchor, through int) {
	last := min(through, copied.count())
	for number := anchor + 1; number <= last; number++ {
		copied.rewrite(number, blankPastIndentation(copied.line(number)))
	}
}

// leftOpenAfter is the quote and flow state a sensitive value leaves open at the end of the line it
// is anchored on, read from that value's own column so that a key written before it -- `"secrets":`
// -- cannot contribute a quote the value did not open.
func leftOpenAfter(written string, start columnInRunes) sensitiveContinuation {
	return sensitiveContinuationAfter(written, byteOffsetOf(written, start))
}

// blankIndentedContinuation blanks the lines a value occupies after the one its node is anchored on.
//
// Two things can carry a value onto a later line, so membership has two rules and the lexical one is
// asked first. While a quote or flow container the anchor line opened is still unclosed, the line
// belongs to the value whatever its indentation: YAML lets a multi-line flow scalar continue at any
// column past the *enclosing block's* indentation, and that is strictly less than the anchor line's
// own whenever the key is nested. An indentation-only walk therefore stopped one line early and
// rendered the second half of `secrets: "PART-A\n     PART-B"` verbatim -- a valid document, on the
// branch that runs for every document that parses
// (TestAMultilineQuotedSecretIsRedactedWhereItsContinuationDedents).
//
// Once nothing is open, indentation is the rule again, because indentation is what carries a block
// scalar, which has no closing token at all. The state is advanced only while it is open: a stray
// apostrophe on a line that belongs to the block by indentation must not re-open a quote and carry
// the blanking past where the block ends.
//
// least is that indentation rule, and it is taken as a parameter rather than derived here because it
// is not a property of the anchor line alone: it depends on whether that line is the value's own
// first line or its key's (leastIndentInsideTheValue).
func blankIndentedContinuation(copied renderCopy, anchor, least int, opened sensitiveContinuation) {
	for number := anchor + 1; number <= copied.count(); number++ {
		written := copied.line(number)
		if isBlankLine(written) {
			// Its own bytes can hide nothing whatever encloses it, and a block scalar keeps
			// it.
			continue
		}

		// A line split off by a lone carriage return is the tail of the physical line above, so the
		// block still covers it whatever its indent looks like. Without this the split tail dedents to
		// column 1, the loop returns, and the rest of the secret is rendered verbatim -- which is the
		// leak FuzzRenderedTextNeverQuotesASecret found on `- |` holding a return.
		continuation := indentWidth(written)
		if !opened.open() && continuation < least && !copied.continuesTheLineAbove(number) {
			return
		}

		copied.rewrite(number, blankPastIndentation(written))
		if opened.open() {
			opened = opened.after(written)
		}
	}
}
