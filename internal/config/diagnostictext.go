package config

import (
	"slices"
	"strings"
)

// This file closes the *second* channel by which text reaches rendered output.
//
// The first is the quoted source line, and redact.go is its choke point (ADR-5). A diagnostic's
// message and hint are the second: they are written beside the quoted lines and are not quoted from
// them, so nothing about the first channel constrains them. Most of them are this package's own
// wording, which no document can influence -- but parse.go reuses the parser's message verbatim,
// and the parser composes that out of the document. A duplicated key written inside a value the
// table declares secret in full was quoted as `[redacted]` on the line above and named in full on
// the caret line.
//
// So the two channels are checked against the same answer: whatever redaction removed from the
// source is removed from the diagnostic text too. That answer is a property of the redaction that
// ran, which is why it is carried on quotableLines rather than recomputed here -- neither channel
// can be contained by a rule the other does not share.

// contained is diagnostic text with every word redaction withheld from the source replaced.
//
// The words are replaced rather than the whole message, because a message names the condition as
// well as the bytes and only the bytes are secret: `mapping key "[redacted]" already defined at
// [7:9]` still tells its reader what to fix. Longest first, so a word that contains a shorter one
// is replaced as itself rather than left holding a placeholder in the middle.
func (q quotableLines) contained(text string) string {
	if text == "" || len(q.withheld) == 0 {
		return text
	}

	words := slices.Clone(q.withheld)
	slices.SortFunc(words, func(a, b string) int { return len(b) - len(a) })

	for _, word := range words {
		text = replacingWholeWords(text, word, redactionPlaceholder)
	}
	return text
}

// replacingWholeWords replaces every occurrence of word that is not part of a longer run of
// identifier characters.
//
// Whole words rather than substrings, because a withheld stretch splits into words as short as one
// character -- `secrets: a` withholds an `a` -- and replacing every `a` in a message would leave
// nothing of it. The boundary is the same one a reader uses: a match surrounded by punctuation,
// quotes or space is the document's word, and one surrounded by letters is part of this package's
// own.
func replacingWholeWords(text, word, replacement string) string {
	if word == "" {
		return text
	}

	var built strings.Builder
	for at := 0; at < len(text); {
		found := strings.Index(text[at:], word)
		if found < 0 {
			built.WriteString(text[at:])
			break
		}
		found += at

		built.WriteString(text[at:found])
		if standsAlone(text, found, found+len(word)) {
			built.WriteString(replacement)
		} else {
			built.WriteString(word)
		}
		at = found + len(word)
	}
	return built.String()
}

// standsAlone reports whether the half-open byte span is bounded by something other than a word
// character on both sides.
func standsAlone(text string, from, past int) bool {
	return (from == 0 || !isDocumentWordByte(text[from-1])) &&
		(past == len(text) || !isDocumentWordByte(text[past]))
}

// isDocumentWordByte is the one predicate this file splits a document's words on and bounds them
// by. It is the document's rule, and deliberately not whenlex.go's isWordByte, which is PostgreSQL's
// -- that one admits a dollar sign and refuses a hyphen, because it answers a different grammar's
// question. One predicate for
// both, because a split that keeps a byte inside a word and a boundary that does not agrees with
// nothing: `PGNOTY-SENTINEL-DUP` would be withheld as three words and each replaced separately,
// rendering the hyphens an author never wrote into the message.
//
// The hyphen is a word byte because a value routinely holds one and no YAML construct ends a token
// with it. A dot is not: it separates the steps of a path, and a locator naming one step must not
// be taken for the whole.
func isDocumentWordByte(letter byte) bool {
	return letter == '_' || letter == '-' || letter >= 0x80 ||
		('0' <= letter && letter <= '9') ||
		('a' <= letter && letter <= 'z') || ('A' <= letter && letter <= 'Z')
}

// withheldWords is every word redaction removed from the source, read by comparing the lines it
// produced against the ones it was given.
//
// A word rather than the stretch it came from, because a diagnostic quotes a *piece* of the
// document: the parser names a duplicated key without the colon or the value that followed it, so a
// test for the whole stretch answers no while the message holds the key verbatim.
func withheldWords(raw, redacted []string) []string {
	var words []string
	for number, line := range raw {
		if number >= len(redacted) || line == redacted[number] {
			continue
		}
		words = append(words,
			strings.FieldsFunc(removedStretch(line, redacted[number]), isNotAWordRune)...)
	}
	return words
}

// removedStretch is the part of a line its redacted form does not hold: what lies between the
// longest prefix and the longest suffix the two share.
//
// Both are counted in bytes, and a count that lands inside a multi-byte rune is deliberately not
// corrected. The result is only ever searched for, so a stretch one byte too wide over-matches --
// the direction redaction is required to err in -- while narrowing it to a rune boundary could
// leave the first byte of a withheld word outside the answer.
func removedStretch(raw, redacted string) string {
	prefix := 0
	for prefix < len(raw) && prefix < len(redacted) && raw[prefix] == redacted[prefix] {
		prefix++
	}

	suffix := 0
	for suffix < len(raw)-prefix && suffix < len(redacted)-prefix &&
		raw[len(raw)-1-suffix] == redacted[len(redacted)-1-suffix] {
		suffix++
	}
	return raw[prefix : len(raw)-suffix]
}

func isNotAWordRune(letter rune) bool {
	return letter <= 0xFF && !isDocumentWordByte(byte(letter))
}

// Contained returns diags with every message and hint redacted against src, exactly as Render redacts
// them on its way to text. It exists because Render is not the only consumer: a caller that formats
// diagnostics itself -- as JSON, as a log record, as a structured event -- reaches the same second
// channel this file closes, and cannot reach the answer redaction gave without it. internal/cli
// formatted its own JSON from Error.Msg and printed a signing secret that the text format had
// replaced. TestContainedRedactsADiagnosticTheSameWayRenderDoes pins this end; internal/cli's own
// diagnosticredaction_test.go holds both of its output formats to one answer.
func (diags Errors) Contained(src []byte) Errors {
	if len(diags) == 0 {
		return diags
	}
	lines := redactedLines(src)
	contained := make(Errors, len(diags))
	for i, diag := range diags {
		diag.Msg, diag.Hint = lines.contained(diag.Msg), lines.contained(diag.Hint)
		contained[i] = diag
	}
	return contained
}
