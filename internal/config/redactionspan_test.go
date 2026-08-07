package config

import (
	"strings"
	"testing"
)

// TestReplacingASpanPastTheLastRuneCanOnlyOverRedact pins what replaceRunes does at the one
// boundary where its two rune columns can disagree with the line it is given. It cannot occur
// at its only call site -- two spans on one line never overlap, so the rightmost-first order
// leaves every column to the left exactly where it was derived -- but the answer is asserted
// rather than assumed, because the safe direction is the whole point: the tail goes with the
// replacement instead of being stitched back on.
func TestReplacingASpanPastTheLastRuneCanOnlyOverRedact(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		start columnInRunes
		count int
		want  string
	}{
		{
			name:  "a span inside the line keeps the text after it",
			line:  "secrets: [aaa, b]",
			start: 11,
			count: 3,
			want:  "secrets: [[redacted], b]",
		},
		{
			name:  "a span reaching past the last rune takes the rest of the line with it",
			line:  "secrets: [aaa",
			start: 11,
			count: 9,
			want:  "secrets: [[redacted]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := replaceRunes(tc.line, tc.start, tc.count, redactionPlaceholder); got != tc.want {
				t.Errorf("replaceRunes(%q, %d, %d) = %q, want %q", tc.line, tc.start, tc.count, got, tc.want)
			}
		})
	}
}

// The header keeps the source coordinate a machine can navigate to. The caret is different: it
// points into the rewritten snippet line, so every length-changing replacement to its left must
// carry that source column onto the rendered line.
func TestACaretRightOfALengthChangingRedactionPointsAtItsToken(t *testing.T) {
	// `database: {url: ` is sixteen runes, so the value starts at column 17 and `pg_bad` at
	// column 45: sixteen, plus the eighteen of `postgres://u:p@h/x`, plus `, schema: `.
	const source = "database: {url: postgres://u:p@h/x, schema: pg_bad}\n"
	const password = "p"
	const sourceColumn = 45

	block := Errors{{File: "flow.yaml", Line: 1, Col: sourceColumn, Msg: "message"}}.Render([]byte(source))

	for _, want := range []string{"postgres://u:[redacted]@h/x", "schema: pg_bad"} {
		if !strings.Contains(block, want) {
			t.Errorf("the block does not contain %q:\n%s", want, block)
		}
	}
	if !strings.HasPrefix(block, "flow.yaml:1:45\n") {
		t.Errorf("header no longer carries the original source coordinate:\n%s", block)
	}

	shift := len(redactionPlaceholder) - runeCount(password)
	wantCaret := len(offendingMarker) + len("1") + len(gutterBar) + sourceColumn + shift
	if got := caretColumn(t, block); got != wantCaret {
		t.Errorf("caret is at column %d, want %d after carrying the replacement delta:\n%s",
			got, wantCaret, block)
	}
	if target := caretTarget(t, block); target != 'p' {
		t.Errorf("caret points at %q, want the first rune of pg_bad:\n%s", target, block)
	}
}

// quotedSnippetLine is the source text of a block's marked snippet line, without its gutter, so a
// test can ask where a token sits in the line a reader actually sees.
func quotedSnippetLine(t *testing.T, block string) string {
	t.Helper()

	for _, line := range strings.Split(block, "\n") {
		after, marked := strings.CutPrefix(line, offendingMarker)
		if !marked {
			continue
		}
		if _, quoted, found := strings.Cut(after, gutterBar); found {
			return quoted
		}
		t.Fatalf("marked line %q carries no gutter bar", line)
	}

	t.Fatalf("no marked snippet line in:\n%s", block)
	return ""
}
