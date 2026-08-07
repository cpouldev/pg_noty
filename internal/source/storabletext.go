package source

import (
	"strings"
	"unicode/utf8"
)

// replacementRune stands in for a byte a destination or an error produced that a text column cannot
// store.
const replacementRune = '\uFFFD'

// storableText replaces every byte a Postgres text column refuses: a NUL, and any sequence that is
// not valid UTF-8 (SQLSTATE 22021).
//
// It covers all three text parameters this package writes -- response_snippet, error, and Dead's
// dead_reason -- because a refused one aborts the settle transaction, which rolls back the queue
// row's delete and leaves the event redelivering forever. Invalid bytes become one replacement rune
// each rather than being dropped, so a reader still sees something was there.
func storableText(text string) string {
	if utf8.ValidString(text) && !strings.ContainsRune(text, 0) {
		return text
	}
	var built strings.Builder
	built.Grow(len(text))
	for index := 0; index < len(text); {
		decoded, width := utf8.DecodeRuneInString(text[index:])
		if decoded == 0 || (decoded == utf8.RuneError && width <= 1) {
			decoded = replacementRune
		}
		built.WriteRune(decoded)
		index += max(width, 1)
	}
	return built.String()
}
