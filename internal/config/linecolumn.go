package config

import (
	"strings"
	"unicode/utf8"
)

// This file holds the two coordinate systems a line of the configuration is measured in, the
// conversion between them, and the one place that conversion is applied to a whole line at once
// (blankPastIndentation).
//
// They are distinct named types rather than two `int`s because D3's fallback branch calls
// functions of both from one switch: a scan over a line's text answers in bytes, while every
// column this package derives, renders or points a caret at is counted in runes (ADR-4). A line
// holding a multi-byte character is where the two disagree, and handing one coordinate where the
// other is expected corrupts the line rather than failing, so the mistake is made a compile error
// instead of a test's problem.

// byteOffset is a position in a line counted in bytes, as a scan over its text reports one.
type byteOffset int

// columnInRunes is a 1-based position in a line counted in runes, which is the only kind of
// column this package derives from a source position.
type columnInRunes int

// byteOffsetOf is where the rune column begins in line, or the line's length when the column is
// past its last rune.
func byteOffsetOf(line string, column columnInRunes) byteOffset {
	current := columnInRunes(firstColumn)
	for offset := range line {
		if current == column {
			return byteOffset(offset)
		}
		current++
	}
	return byteOffset(len(line))
}

// runesFrom is the text of a line from a rune column onward.
func runesFrom(line string, column columnInRunes) string {
	return line[byteOffsetOf(line, column):]
}

// byteSpanOf is where a run of count runes beginning at a rune column starts and ends in line. The
// second offset is derived by continuing from the first rather than by counting from the line's start
// again, so a span is located in one pass over the text it spans.
func byteSpanOf(line string, start columnInRunes, count int) (byteOffset, byteOffset) {
	from := byteOffsetOf(line, start)
	past := byteOffsetOf(line[from:], columnInRunes(count)+firstColumn)

	return from, from + past
}

// indentCharacters are the two characters a YAML line may be indented with. A tab is illegal as
// YAML indentation, but the fallback branch reads exactly the documents that hold illegal things,
// so one is counted rather than refused.
const indentCharacters = " \t"

// pastIndentation is a line's content and how many indent runes precede it. The two are answered
// together because every caller that needs one needs the other, and deriving them separately is how
// the same subtraction came to be written in two places.
func pastIndentation(line string) (string, int) {
	content := strings.TrimLeft(line, indentCharacters)

	return content, runeCount(line) - runeCount(content)
}

// indentWidth is how many indent runes a line opens with.
func indentWidth(line string) int {
	_, indent := pastIndentation(line)

	return indent
}

// blankPastIndentation replaces everything a line holds past its own indentation. It is what both
// redaction branches do to a line they have decided a sensitive value covers but whose extent
// within that line they will not guess at, so the line keeps its shape in a snippet and none of
// its content.
//
// It lives here, beside indentWidth, because what the nine call sites it replaces each spelled out
// was this file's conversion -- an indentation counted in runes, turned into the 1-based column
// just past it -- applied to the very line the count came from. Spelling a conversion at each
// caller is how the same question comes to be asked in two conventions: the package already holds
// one such pair, containerinterior.go and writtenstart.go writing this one's operands in opposite
// orders, and three copies of source.go's neighbouring continuesTheLineAbove question had already
// diverged by an index before they were consolidated (source.go). Here a one-position shift is a
// rendered secret.
func blankPastIndentation(line string) string {
	return blankFromColumn(line, columnInRunes(indentWidth(line))+firstColumn)
}

// runeCount is how many runes a string holds.
func runeCount(text string) int {
	return utf8.RuneCountInString(text)
}
