package config

import (
	"slices"
	"strings"
)

// This file answers what shape a line of an unparseable document is, for D3's fallback branch --
// and, in isBlankLine alone, the one shape question the path-aware branch's walks ask too.
//
// It exists because the fallback makes two decisions about each line -- does this line close the
// block a sensitive key opened, and may this line be rendered as written -- and the shapes that
// answer the first are not the shapes that answer the second. Collapsing them into one predicate is
// what rendered a secret verbatim through three reviews: "a comment does not close a block" is true,
// and it was allowed to imply "a comment holds nothing to hide", which inside a block scalar is
// false. Each predicate here therefore states one claim and only the claim its own bytes support.

// sequenceItemMarker introduces a list item, which is the one line shape that can sit at its
// key's own indentation and still belong to it.
const sequenceItemMarker = "-"

// commentIndicator opens a comment -- outside a block scalar. It is the one token below that a line
// is exempted for by *prefix* rather than as the whole of it, which is why the containment oracle
// reads this constant rather than writing a `#` of its own: a generated secret has to be able to put
// one at the start of a line, or the arm it governs is a branch no run can reach.
const commentIndicator = "#"

// documentMarkers punctuate a YAML stream rather than hold a value. They are named so that a line
// holding one is read as structure instead of as an unreadable token, which would blank it out of a
// snippet that needs it -- and named individually so a test can say which one it planted.
const (
	documentStart = "---"
	documentEnd   = "..."
)

var documentMarkers = []string{documentStart, documentEnd}

// writtenContent is a line's content without the whitespace it ends in, which is what decides its
// shape: `---   ` punctuates a stream exactly as `---` does. It is one formula rather than a
// TrimRight at each reader, so no two of them can disagree about where a line's content stops.
func writtenContent(content string) string {
	return strings.TrimRight(content, indentCharacters)
}

// isBlankLine reports whether a whole line is blank by Unicode's account of whitespace.
//
// It is the rule the three walks that step over a value's lines share: a blank line neither ends
// the value they are inside nor holds anything to blank, so it is skipped rather than written to
// (blockextent.go, and both walks in sensitivereach.go). Naming it puts that rule in one place,
// where it was three unnamed copies of one expression carrying one rationale.
//
// It is NOT the blank half of holdsNoBytesToHide below, and the two must not be folded together.
// This trims by unicode.IsSpace; that one trims writtenContent's space and tab only, so a line
// holding just a vertical tab, a form feed or a non-breaking space is blank here and is not there.
// Folding this rule into that one would widen it, and a line the fallback branch blanks inside an
// open sensitive block would stop being blanked -- under-redaction, which is the one answer that
// branch may never give.
func isBlankLine(line string) bool {
	return strings.TrimSpace(line) == ""
}

// holdsNoBytesToHide reports whether a line's content is a shape that cannot carry a secret in any
// context: a line with nothing written on it, or one whose whole content is a document marker.
//
// Neither is exempted on trust, which is what separates them from a comment. A blank line has no
// bytes to leak. A document-marker line holds exactly `---` or `...` -- three characters that are
// not a secret by any account, and that a snippet needs, because two_documents.golden quotes one as
// a context line. A comment's bytes are arbitrary, so no such statement can be made about it.
func holdsNoBytesToHide(content string) bool {
	written := writtenContent(content)

	return written == "" || slices.Contains(documentMarkers, written)
}

// looksLikeAComment reports whether a line's content opens with the comment indicator -- and only
// that it looks like one.
//
// Inside a block scalar, or inside a quoted scalar written across several lines, such a line is
// content and not a comment at all. The fallback reads exactly the documents where nothing can tell
// the two apart, so wherever a sensitive key's block could reach it, this shape is treated as the
// value it may be.
func looksLikeAComment(content string) bool {
	return strings.HasPrefix(writtenContent(content), commentIndicator)
}

// closesNoBlock reports whether a line's content is one of the shapes that leaves the block a
// sensitive key opened still open: a blank line, a comment, or a document marker. A comment or a
// blank line between list items leaves the items beneath their key, and that is the whole of the
// claim -- whether such a line may also be rendered as written is coveredByBlockOpenedAt's separate
// answer.
func closesNoBlock(content string) bool {
	return holdsNoBytesToHide(content) || looksLikeAComment(content)
}
