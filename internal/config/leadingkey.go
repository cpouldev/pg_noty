package config

import "strings"

// This file answers one question for D3's fallback branch: what is the token a line opens with,
// and can the key it names be read out of the raw text with certainty?
//
// It exists because "not a sensitive key", "a key whose name I cannot read" and "no key at all"
// are three different answers and only the first is safe to keep verbatim. The fallback stands in
// for a parser, so a name it cannot read may be a sensitive one: a double-quoted key can spell
// `url` with a numeric escape, and then none of those three letters appears in the line at all.
// Every judgement here is therefore biased towards *unreadable* -- an unusual but legal key is
// blanked, which over-redacts, while a mistaken certainty leaks.

// leadingToken is what a line's leading token turned out to be. It is three-valued rather than a
// boolean because the safe answer differs between the last two: an unreadable key may be a
// sensitive one wherever it appears, while a line that is no key at all can only hold a secret
// where a sensitive key's block already covers it.
type leadingToken uint8

const (
	// aKeyThisFileCanRead is a mapping key whose name is exactly the bytes written before its
	// colon, in any of the spellings YAML permits.
	aKeyThisFileCanRead leadingToken = iota
	// aKeyThisFileCannotRead is a token that ends in a quote this file cannot resolve, so
	// neither the name nor where the token ends is the text as written.
	aKeyThisFileCannotRead
	// noKeyAtAll is every other token: a flow collection opening, a block scalar indicator, or
	// a plain scalar carrying no key's colon -- which is the shape a value written on a line of
	// its own leaves behind.
	noKeyAtAll
)

// leadingTokenOf classifies the token a line's content opens with.
func leadingTokenOf(content string) leadingToken {
	scalar := content[sequenceMarkerWidth(content):]

	if strings.HasPrefix(scalar, `"`) || strings.HasPrefix(scalar, `'`) {
		return quotedLeadingToken(scalar)
	}
	if beginsWithUnreadableKeyIndicator(scalar) {
		return aKeyThisFileCannotRead
	}
	if opensWithAPlainKey(scalar) {
		return aKeyThisFileCanRead
	}
	return noKeyAtAll
}

// quotedLeadingToken classifies a token opening with a quoted scalar.
//
// It can be read only when its quote closes plainly. An escape means neither the name nor where
// the token ends is the text as written: a backslash escape inside double quotes spells a name
// the raw bytes do not contain, and a doubled apostrophe inside single quotes stands for one
// apostrophe, so the quote that really closes the token sits past the pair. Either way the
// answer is unreadable rather than guessed at. A quote that never closes belongs to a scalar
// written across several lines, which carries no key.
func quotedLeadingToken(scalar string) leadingToken {
	quote, body := scalar[:1], scalar[1:]

	end := strings.Index(body, quote)
	if end < 0 {
		return noKeyAtAll
	}
	if strings.Contains(body[:end], `\`) || strings.HasPrefix(body[end+1:], quote) {
		return aKeyThisFileCannotRead
	}

	// YAML needs no space after a quoted key's colon -- `"url":x` is JSON and JSON is YAML --
	// so the colon is the whole of what makes this token a key.
	if !opensWithAColon(body[end+1:]) {
		return noKeyAtAll
	}
	return aKeyThisFileCanRead
}

// plainScalarEnders end a plain scalar before its colon could make it a key: the flow punctuation,
// and `#`. A `#` opens a comment only after whitespace, so treating every one as an ender reports
// the legal key `a#b` as no key -- which over-redacts its line, the direction this branch errs in.
const plainScalarEnders = ",[]{}#"

// opensWithAPlainKey reports whether this text opens with an unquoted scalar used as a key. A plain
// key ends at a colon that whitespace or the line's end follows, which is YAML's own rule:
// `postgres://host/db` holds two colons and is a scalar rather than a key, which is exactly what a
// value written on a line of its own looks like.
func opensWithAPlainKey(scalar string) bool {
	for at := 0; at < len(scalar); at++ {
		if strings.IndexByte(plainScalarEnders, scalar[at]) >= 0 {
			return false
		}
		if scalar[at] == ':' && whitespaceOrEndFollows(scalar[at+1:]) {
			// A name is at least one character, so a colon at the very start is the explicit
			// key indicator rather than a key of its own.
			return at > 0
		}
	}
	return false
}

// opensWithAColon reports whether what follows a quoted name is the colon that makes it a key.
func opensWithAColon(after string) bool {
	return strings.HasPrefix(strings.TrimLeft(after, indentCharacters), ":")
}

// sequenceMarkerWidth is how many bytes of sequence item marker this text opens with. A mapping may
// be written on a sequence item's own line and a sequence may nest, so the markers are stepped over
// before the key is read.
func sequenceMarkerWidth(content string) int {
	width := 0

	for opensAnItem(content[width:]) {
		// indentWidth counts runes, which is the same number as bytes here: every character it
		// counts is one byte long.
		past := width + len(sequenceItemMarker)
		width = past + indentWidth(content[past:])
	}
	return width
}

// opensAnItem reports whether text opens with a sequence item marker: a `-` that whitespace or the
// line's end follows, rather than one opening a plain scalar such as `-1`.
func opensAnItem(text string) bool {
	rest, found := strings.CutPrefix(text, sequenceItemMarker)
	return found && whitespaceOrEndFollows(rest)
}

// whitespaceOrEndFollows reports whether text is empty or opens with indentation. It is what
// separates a token from what follows it in block YAML: a plain key's colon and a sequence item's
// `-` are each only themselves when this holds.
func whitespaceOrEndFollows(text string) bool {
	return text == "" || strings.IndexByte(indentCharacters, text[0]) >= 0
}
