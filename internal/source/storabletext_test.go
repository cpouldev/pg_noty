package source

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestEveryTextParameterIsStorable covers the encoding contract of the three text columns this
// package writes. A Postgres text column refuses a NUL byte and refuses invalid UTF-8 (22021), and
// recordDelivery writes response_snippet and error inside the settle transaction while Dead writes
// dead_reason -- so a refused parameter rolls back the queue row's delete, the row stays delivering,
// lease reclaim returns it, and the event is redelivered and refused identically forever.
func TestEveryTextParameterIsStorable(t *testing.T) {
	for _, testCase := range []struct{ name, text string }{
		{name: "a NUL byte, which text refuses whatever the encoding", text: "ok\x00fine"},
		{name: "a lone continuation byte", text: string([]byte{0x80, 0x81})},
		{name: "a truncated three-byte rune, as a byte cap leaves behind", text: "caf\xc3"},
		{name: "raw 0xff, which no UTF-8 sequence contains", text: string([]byte{0xff, 0xfe})},
		{name: "valid multi-byte text", text: "café ☕"},
		{name: "empty", text: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stored := storableText(testCase.text)
			if !utf8.ValidString(stored) {
				t.Errorf("storableText(%q) = %q, which is not valid UTF-8", testCase.text, stored)
			}
			if strings.ContainsRune(stored, 0) {
				t.Errorf("storableText(%q) = %q, which holds a NUL", testCase.text, stored)
			}
		})
	}
}

// TestStorableTextLeavesCleanTextAlone is the near-miss the guard must survive: text a column
// accepts must come back byte-identical, or the sanitiser is corrupting forensics it was meant to
// preserve.
func TestStorableTextLeavesCleanTextAlone(t *testing.T) {
	for _, clean := range []string{"", "plain ASCII", "café ☕ 日本語", "quotes \"and\" 'marks'", "tab\tnewline\n"} {
		if got := storableText(clean); got != clean {
			t.Errorf("storableText(%q) = %q, want it unchanged", clean, got)
		}
	}
}
