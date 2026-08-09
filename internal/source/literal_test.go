package source

import (
	"strings"
	"testing"
)

func TestQuoteLiteralUsesTheTwoPostgresLiteralBranches(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{name: "empty", input: "", want: "''"},
		{name: "plain word", input: "word", want: "'word'"},
		{name: "one embedded quote", input: "O'Reilly", want: "'O''Reilly'"},
		{name: "two adjacent quotes", input: "a''b", want: "'a''''b'"},
		{name: "one backslash", input: `a\b`, want: `E'a\\b'`},
		{name: "quote and backslash", input: "a'\\b", want: `E'a''\\b'`},
		{name: "multibyte without NUL", input: "π🙂", want: "'π🙂'"},
		// The bare trailing backslash is the input a quote-doubling-only helper leaves
		// unterminated when standard_conforming_strings=off; it forces the E'' branch.
		{name: "bare trailing backslash", input: `tail\`, want: `E'tail\\'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := quoteLiteral(tc.input); got != tc.want {
				t.Errorf("quoteLiteral(%q) = %q, want %q derived from the literal grammar",
					tc.input, got, tc.want)
			}
		})
	}
}

func FuzzQuoteLiteral(f *testing.F) {
	for _, seed := range []string{"", "word", "'", `\`, `a'\b`, `tail\`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got := quoteLiteral(input)
		if !strings.HasSuffix(got, "'") {
			t.Fatalf("quoteLiteral(%q) = %q, which has no closing delimiter", input, got)
		}
		if strings.Contains(input, `\`) && !strings.HasPrefix(got, "E'") {
			t.Fatalf("quoteLiteral(%q) = %q, want the escape-string branch", input, got)
		}
		if !strings.Contains(input, `\`) && !strings.HasPrefix(got, "'") {
			t.Fatalf("quoteLiteral(%q) = %q, want the plain branch", input, got)
		}
	})
}
