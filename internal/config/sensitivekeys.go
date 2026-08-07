package config

import "strings"

// This file is D3's fallback branch: when the document does not parse there are no paths to
// resolve, so a value is treated as secret because of the key *name* above it. It reads lines as
// text, because text is all a document the parser rejected leaves behind.
//
// D3 states this branch's contract in the negative -- "over-redaction is acceptable in that one
// case; under-redaction never is" -- so all three axes of the decision fail closed:
//
//   - Block membership. Every line an open sensitive block still covers goes, whatever shape it
//     turns out to be. The membership question is asked before any exemption, because an exemption
//     granted for one decision must not silently answer another.
//   - Line shape. Outside such a block, the shapes that hold nothing are kept by name -- and those
//     names are held to what their own bytes support, which lineshape.go decides.
//   - Key spelling. Asked once, of sensitivekeyforms.go, which owns it for both branches and
//     answers for every spelling YAML permits -- including the explicit-key form, whose colon sits
//     on a line the name is not on. That the recognised set is the grammar's rather than the
//     matcher's is asserted by TestBothBranchesContainASecretWrittenInEverySpellingTheGrammarPermits,
//     which takes its twelve spellings from YAML 1.2 and runs each on both branches
//. Beyond those, a line that *is* a key
//     whose name cannot be read from the raw bytes is blanked rather than kept, because that name
//     may decode to a sensitive one, and a line that is no key at all is blanked wherever an open
//     sensitive block could reach it. A text matcher standing in for a parser must treat what it
//     cannot classify as a value.
//
// All three are answered here because the path-aware branch answers all three -- it reads the
// parser's own decoded key name, so `"url":`, `'url':` and `url:` are all the key `url` there --
// and the two branches must not disagree on any input class.

// outsideSensitiveBlock is the absence of an open block rather than an indentation width: no
// line is indented by a negative number of columns, so it cannot be read as one.
const outsideSensitiveBlock = -1

// redactBySensitiveKeyName blanks every value a sensitive key name can reach: the rest of the
// line the key is written on, and every line of the block that key opens -- the items of its
// list, the lines of a block scalar or of a scalar quoted across several lines, and a value
// written on the lines below the key rather than beside it.
//
// It over-redacts in five deliberate ways, none of which can under-redact. `url` is the name of
// a sensitive key and of a public one, so both go (Implementation Note 8). A matched value is
// blanked to the end of its line rather than to the end of the value, because guessing where a
// value ends in text the parser itself rejected is exactly the guess that leaks. A line inside
// the block is blanked from its first non-space rune, which takes its `-` or its nested key name
// with it: in text the parser rejected, what reads as an item marker may be the first character
// of a secret. A comment-shaped line goes wherever an open block could reach it, at any
// indentation, because inside a block scalar it is content. And a line whose key is written in a
// form no raw-text scan can decode is blanked whole, because the name it decodes to may be a
// sensitive one -- see keptVerbatim for exactly which unmatched lines go and which are kept.
//
// Each arm of the walk answers two questions -- what this line renders as, and what lexical state
// the next line inherits -- and every arm answers both, including the one that inherits nothing and
// says so. A quote or a flow container can be opened on *any* line of a sensitive block, not only on
// the key's own line, and the arm that blanks a line by indentation used to hand on the state it was
// given: the quote went untracked, the block's indentation rule ended at the next dedent, and the
// scalar's key-shaped continuation was rendered verbatim.
//
// It returns lines of its own rather than writing the ones it is given, so the source's slice
// cannot be the one a replacement lands in even when it is the slice handed in.
func redactBySensitiveKeyName(lines []string, continuesPhysicalLine []bool) []string {
	redacted := make([]string, 0, len(lines))
	state := fallbackLineState{
		blockIndent:  outsideSensitiveBlock,
		continuation: noSensitiveContinuation,
	}

	for number, line := range lines {
		// A line split off by a lone carriage return is the tail of the physical line above, not a line of
		// its own: the person who wrote the file sees one line, and the bytes after the return are on it.
		// Asked before every other question so that a tail cannot dedent out of the block covering it --
		// which is how a planted secret came to be rendered verbatim after `- |`. Asked through the
		// package's one answer, which is 1-based like every other line number here. This was written
		// inline against a 0-based index, a third spelling of the question already diverging from the
		// other two in convention.
		continuesAbove := continuesTheLineAbove(continuesPhysicalLine, number+1)

		var rewritten string
		rewritten, state = redactedFallbackLine(line, continuesAbove, state)
		redacted = append(redacted, rewritten)
	}
	return redacted
}

// redactedFallbackLine is what one line renders as, and the state the line after it inherits
// (fallbackstate.go, which is why both leave together).
func redactedFallbackLine(
	line string,
	continuesAbove bool,
	state fallbackLineState,
) (string, fallbackLineState) {
	content, indent := pastIndentation(line)

	switch {
	case state.continuation.open():
		// A key-shaped line at the sensitive key's own indentation is still scalar content
		// until its quote and flow containers close. It is blanked before YAML indentation
		// can close the block.
		return blankPastIndentation(line), state.blanked(line)
	case continuesAbove && !holdsNoBytesToHide(content):
		// A fragment of a physical line, so none of the exemptions below may answer for it:
		// every one of them is a statement about a line somebody wrote, and this is half of
		// one. The name itself can be split by the break -- `sec<CR>rets: <value>` writes a
		// sensitive key that neither fragment spells, so both fragments were certified readable
		// keys and the value was rendered verbatim.
		//
		// The gate this replaces asked whether the *previous* line had opened a block or hidden
		// a tail, which answers "is this fragment already covered" and not "may this fragment
		// exempt itself". It was false for exactly the class above, because a fragment holding
		// no readable key opens nothing.
		return blankPastIndentation(line), state.blanked(line)
	case coveredByBlockOpenedAt(indent, content, state.blockIndent):
		// Blanked from its first non-space rune, whatever shape the line turns out to
		// be. This arm is first so that no exemption below can answer for a line the
		// block still covers.
		return blankPastIndentation(line), state.blanked(line)
	case closesNoBlock(content):
		// No block covers it and it closes none, so no sensitive name governs this
		// line and the shape holds nothing: it is rendered as written.
		//
		// The state it hands on is stated rather than left to fall out of the block. The
		// continuation cannot differ: the first arm did not fire, and only the empty
		// continuation is closed -- which TestOnlyTheEmptyContinuationIsClosed pins,
		// so a field added outside open()'s reading fails there first. The block indentation
		// is carried unchanged, which is this arm's whole name: a blank line, a comment or a
		// document marker leaves the block a sensitive key opened still open.
		return line, fallbackLineState{
			blockIndent:  state.blockIndent,
			continuation: noSensitiveContinuation,
		}
	default:
		rewritten, opened, continued := redactedOutsideAnyBlock(
			line, content, indent, state.blockIndent)

		return rewritten, fallbackLineState{blockIndent: opened, continuation: continued}
	}
}

// coveredByBlockOpenedAt reports whether a line still belongs to the block a sensitive key opened at
// blockIndent -- which is every line until one closes it.
//
// Indentation is YAML's own scoping: anything indented further is inside the block, and a sequence
// item at the key's own column is inside it too, because YAML permits a list there. A comment-shaped
// line is inside as well, wherever it sits: it closes no block, and in text the parser rejected it
// is as likely to be a line of a block scalar as a comment. That is the exemption scoped to the
// decision it was reasoned about -- "does not close the block" -- and not to the one it cannot
// support.
//
// A blank line and a document marker are asked first and answered no, because neither can carry a
// secret whatever encloses it, and blanking them would cost a snippet context it needs while
// protecting nothing.
func coveredByBlockOpenedAt(indent int, content string, blockIndent int) bool {
	if blockIndent == outsideSensitiveBlock || holdsNoBytesToHide(content) {
		return false
	}
	if indent > blockIndent || looksLikeAComment(content) {
		return true
	}
	return indent == blockIndent && strings.HasPrefix(content, sequenceItemMarker)
}
