package config

// This file answers one question for D3's fallback branch: a line that no sensitive key's block
// covers -- what does it render as, and does it open a block of its own?
//
// It is separate from the walk in sensitivekeys.go because the walk decides *membership* (which
// lines an already-open block still reaches) while this decides *entry* (which lines open one, and
// which of the rest may be shown as written). Collapsing the two is what let an exemption reasoned
// about for one of them silently answer the other.

// redactedOutsideAnyBlock answers a line that no sensitive key's block covers: what it renders
// as, and the indentation of the block it opens -- outsideSensitiveBlock when it opens none.
//
// The key-spelling axis is asked once, of its one owner, and answers for every spelling YAML
// permits. Two of those spellings write the name and its colon on the same line, so what the name
// governs is the rest of that line; the rest write the colon on a line of its own, so what the name
// governs is that line and the block it opens. Asking the owner once is what keeps this file from
// having an opinion of its own about which spellings exist -- the opinion it held, and which
// disagreed with the path-aware branch for three rounds.
//
// Only when no sensitive name governs the line at all does the line-shape axis decide, which is
// what makes the key-spelling axis fail closed the way the line-shape one does.
func redactedOutsideAnyBlock(
	line, content string,
	indent, blockIndent int,
) (string, int, sensitiveContinuation) {
	switch at, governs := governanceOf(line, content); governs {
	case theValueOnThisLine:
		return blankValueAfter(line, at), indent, sensitiveContinuationAfter(line, at)
	case thisLineAndTheBlockBelow:
		return blankPastIndentation(line), indent, sensitiveContinuationAfter(line, at)
	}

	token := leadingTokenOf(content)
	if token == aKeyThisFileCannotRead || containsUnreadableFlowKey(line) {
		return blankPastIndentation(line), indent, sensitiveContinuationAfter(line, 0)
	}

	if keptVerbatim(token, blockIndent) {
		return line, outsideSensitiveBlock, noSensitiveContinuation
	}

	// The whole line is blanked, so the quote or flow container it leaves open is read from the whole
	// of it -- the same answer the unreadable-key arm above gives, and for the same reason. This arm
	// used to hand on noSensitiveContinuation, which is the one state a blanked line cannot be in:
	// `secrets:` followed by a line opening a quote that never closes reached here, the open quote
	// was discarded, and the next line -- still inside that scalar, as the parser's own message says
	// -- was certified a readable key and rendered verbatim. Run by
	// TestAnUnclosedQuoteBelowASensitiveKeyKeepsCoveringTheLinesAfterIt.
	return blankPastIndentation(line), blockIndent, sensitiveContinuationAfter(line, 0)
}

// keptVerbatim reports whether a line that names no sensitive key may be rendered as it was
// written. The scan above has already matched every spelling of every sensitive name, so what is
// left to decide is what an *unmatched* line means.
func keptVerbatim(token leadingToken, blockIndent int) bool {
	switch token {
	case aKeyThisFileCanRead:
		// The name is exactly what is written and the scan did not match it, so nothing on
		// this line is governed by a sensitive name.
		return true
	case noKeyAtAll:
		// Below an open sensitive block this is that key's value written at an indentation
		// YAML does not allow -- and invalid YAML is the only kind this branch ever reads --
		// so it goes. Outside one, no sensitive name governs it: blanking it would protect
		// nothing and cost a diagnostic its own text, which is all the offending line of a
		// document that is one long plain scalar consists of.
		return blockIndent == outsideSensitiveBlock
	default:
		// A key whose name may be a sensitive one written in a form only a parser can decode,
		// so the value beside it may be a secret wherever this line sits. A token kind added
		// later lands here too, which is the safe side to default to.
		return false
	}
}
