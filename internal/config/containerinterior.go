package config

import "strings"

// This file answers one question about a container the schema declares sensitive in full: does its
// written interior hold runes that none of its children claim?
//
// `secrets: [a, b]` holds none -- every rune between the elements is the flow grammar's own
// punctuation. `secrets: [a,&name b]` holds an anchor name, `secrets: [a, !!str b]` a tag, and a
// block sequence with a comment between its items holds that comment. Stage E drops an anchor
// property from the tree before the walk reads it, so those runes belong to no element: replacing
// the elements alone renders them, inside a value the table declares secret in full.
//
// The question is the class's rather than the reproduction's. The reproduction's container closed
// on a *later line*, which is incidental -- the same runes fit between two elements of a one-line
// container, before the first element of one, and between two items of a block sequence, for which
// the parser reports no closing indicator at all. A condition written on the closing line was
// therefore inert for every block container and blind to every one-line one.
//
// The last line of the interior is the line the container's own delimiter sits on, and which
// delimiter that is depends on the style: a flow container writes a closing bracket the parser
// reports, and a block container is delimited by indentation (blockextent.go). Deriving it from the
// *children* instead answers both styles with the last line a child is anchored on, which is the
// last line of the container only where nothing follows the last child -- so a note an author wrote
// after the last item of a block sequence was outside a scan that read as if it covered the whole
// interior.

// containerPunctuation is every rune YAML writes *between* the children of a collection: the four
// flow delimiters, the entry separator, the block sequence's item marker, the two key indicators,
// and the two block scalar indicators. Anything else written inside a container and claimed by no
// child is text somebody typed -- a name, a tag suffix, a comment -- and a value declared secret in
// full owns it.
const containerPunctuation = "[]{},-:?|>"

// childToken is one child's own written extent on one line, in rune columns.
type childToken struct {
	from columnInRunes
	past columnInRunes
}

// containerHoldsUnclaimedBytes reports whether the interior of the container written at start holds
// a rune outside every child's own token that the grammar did not put there.
//
// start is where the container's own text begins -- its opening bracket in flow style, its first
// item marker in block style -- so a key written before it on the same line is outside the interior
// and is not this value's to hide. Above that line the scan runs to start.reachesBackTo, the first
// line of the block the container's key opened: a container written *below* its key is anchored on
// its own first token, so a note an author wrote between the colon and that token is inside the
// value and outside the container's own text (writtenstart.go).
// closesOn is the line the parser reports the container's closing indicator on, or
// noClosingIndicator for a block container, which writes none.
func containerHoldsUnclaimedBytes(
	text *source,
	start writtenStart,
	found []sensitiveValue,
	closesOn int,
) bool {
	children := childrenWrittenInside(start, found)
	last := lastLineOfTheInterior(text, start, closesOn)
	closesWithADelimiter := closesOn != noClosingIndicator

	for number := start.reachesBackTo; number <= last; number++ {
		written := text.Line(number)
		tokens := childTokensOn(children, number)

		from := firstColumn + columnInRunes(indentWidth(written))
		if number == start.line {
			from = start.column
		}
		past := interiorEndsAt(written, number, last, tokens, closesWithADelimiter)
		if holdsUnclaimedRunes(written, from, past, tokens) {
			return true
		}
	}
	return false
}

// childrenWrittenInside drops the children this container did not have written into it.
//
// A child located *before* the container's own first line was written somewhere else and reduced
// into it, because an alias resolves to the node its anchor defined. Counting it would stretch the
// interior over lines belonging to another value entirely. Nothing is lost on the other side: YAML
// resolves an alias backwards, so a child located at or after this line was reached through a site
// inside the container and therefore sits inside it too.
func childrenWrittenInside(start writtenStart, found []sensitiveValue) []sensitiveValue {
	inside := make([]sensitiveValue, 0, len(found))
	for _, child := range found {
		if child.line >= start.line {
			inside = append(inside, child)
		}
	}
	return inside
}

// lastLineOfTheInterior is the last line the container's own text occupies, bounded by the
// delimiter the grammar writes at this position rather than by the container's members.
//
// A flow container's closer is reported by the parser, so it is the answer and it is taken
// unchanged -- a `# note` an author wrote past the last element but before that closer is then
// inside the interior, where a members-derived bound put it outside. A block container writes no
// closer and is delimited by its indentation, which blockextent.go reads. Both shapes are run by
// TestEveryCellOfTheContainerInteriorGridIsContained's after-the-last-element cells.
func lastLineOfTheInterior(text *source, start writtenStart, closesOn int) int {
	if closesOn != noClosingIndicator {
		return closesOn
	}
	return lastLineOfTheValueAt(text, start.line, start.beginsOnALineOfItsOwn)
}

// interiorEndsAt is the rune column the interior stops before on one line.
//
// Every line but the last runs to the end, because whatever follows a child there is still inside
// the container. The last line stops after the last child written on it **only where the container
// writes a closer**, so a flow container's own bracket -- and a sibling key or a comment an author
// wrote past it -- stay outside a question about what is inside.
//
// A block container writes no closer, so nothing on that line is past it and the line runs to its
// end. Applying the flow bound to a block container dropped every rune after its last child, and a
// null child has no width at all: `secrets:` holding one item written `- ` followed by a comment
// stopped the interior at the `#`, so the comment -- author-written text inside a value the table
// declares secret in full -- was rendered verbatim. The bound was written for the shape that has a
// closer and silently governed the shape that has none. Both shapes are run by
// TestASensitiveContainerHoldingRunesNoChildClaimsIsBlankedWholesale.
func interiorEndsAt(
	written string,
	number, last int,
	tokens []childToken,
	closesWithADelimiter bool,
) columnInRunes {
	endOfLine := firstColumn + columnInRunes(runeCount(written))
	if number != last || len(tokens) == 0 || !closesWithADelimiter {
		return endOfLine
	}

	past := columnInRunes(0)
	for _, token := range tokens {
		past = max(past, token.past)
	}
	return min(past, endOfLine)
}

// childTokensOn is the written extent of every child anchored on one line.
func childTokensOn(children []sensitiveValue, number int) []childToken {
	tokens := make([]childToken, 0, len(children))
	for _, child := range children {
		if child.line != number {
			continue
		}
		tokens = append(tokens, childToken{
			from: child.column,
			past: child.column + columnInRunes(runeCount(child.text)),
		})
	}
	return tokens
}

// holdsUnclaimedRunes reports whether any rune of the half-open column range is neither covered by a
// child's token nor punctuation the grammar wrote.
func holdsUnclaimedRunes(written string, from, past columnInRunes, tokens []childToken) bool {
	for offset, letter := range []rune(runesFrom(written, from)) {
		at := from + columnInRunes(offset)
		if at >= past {
			return false
		}
		if coveredByAToken(at, tokens) || isContainerPunctuation(letter) {
			continue
		}
		return true
	}
	return false
}

func coveredByAToken(at columnInRunes, tokens []childToken) bool {
	for _, token := range tokens {
		if at >= token.from && at < token.past {
			return true
		}
	}
	return false
}

func isContainerPunctuation(letter rune) bool {
	return letter == ' ' || letter == '\t' || strings.ContainsRune(containerPunctuation, letter)
}
