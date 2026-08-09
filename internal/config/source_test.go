package config

import (
	"bytes"
	"testing"
)

func TestSourceBytesAreNotWrittenAfterConstruction(t *testing.T) {
	original := []byte("database:\n  url: ${DATABASE_URL}\n")
	src := newSource("listeners.yaml", original)
	before := string(src.CloneBytes())

	// The operations a renderer performs: read the raw lines, then the raw bytes.
	// Doing them twice must leave the source byte for byte as constructed.
	for range 2 {
		for line := 1; line <= 3; line++ {
			_ = src.Line(line)
		}
		_ = src.CloneBytes()
	}

	if after := string(src.CloneBytes()); after != before {
		t.Errorf("source bytes changed from %q to %q", before, after)
	}
	if !bytes.Equal(src.CloneBytes(), original) {
		t.Errorf("source bytes = %q, want %q", src.CloneBytes(), original)
	}
}

func TestSourceIgnoresMutationOfTheBytesItHandsOut(t *testing.T) {
	src := newSource("listeners.yaml", []byte("version: 1\n"))

	src.CloneBytes()[0] = 'X'

	if got := src.Line(1); got != "version: 1" {
		t.Errorf("Line(1) = %q after writing through CloneBytes(), want %q", got, "version: 1")
	}
	if got := string(src.CloneBytes()); got != "version: 1\n" {
		t.Errorf("CloneBytes() = %q after being written through, want %q", got, "version: 1\n")
	}
}

func TestSourceIgnoresLaterMutationOfTheCallersBytes(t *testing.T) {
	data := []byte("version: 1\n")
	src := newSource("listeners.yaml", data)

	data[0] = 'X'

	if got := src.Line(1); got != "version: 1" {
		t.Errorf("Line(1) = %q after mutating the caller's slice, want %q", got, "version: 1")
	}
}

// TestSourceLineMapsALineNumberToItsRawText covers what a renderer quotes and what a
// column is counted over. The CRLF row is the one a naive split gets wrong: it leaves a
// carriage return on the end of every line, which would reach rendered output verbatim
// and count as a character the reader cannot see.
func TestSourceLineMapsALineNumberToItsRawText(t *testing.T) {
	tests := []struct {
		name string
		text string
		line int
		want string
	}{
		{name: "first line", text: "first\nsecond\n", line: 1, want: "first"},
		{name: "second line", text: "first\nsecond\n", line: 2, want: "second"},
		{name: "line zero has no text", text: "first\nsecond\n", line: 0, want: ""},
		{name: "negative line has no text", text: "first\nsecond\n", line: -1, want: ""},
		{name: "line past the end has no text", text: "first\nsecond\n", line: 99, want: ""},
		{name: "crlf line drops its carriage return", text: "first\r\nsecond\r\n", line: 1, want: "first"},
		{name: "last crlf line drops its carriage return", text: "first\r\nsecond\r\n", line: 2, want: "second"},
		// A lone carriage return is a line *break*, not content: YAML 1.2 §5.4 makes CR, LF and CRLF
		// all breaks and the parser counts them that way, so "fi\rrst" is two lines. This row asserted
		// the opposite until the divergence it encoded was traced to a live defect -- the parser's line
		// number then exceeded this slice, Line answered "", and runeColumn collapsed every column on
		// that line to 1, merging genuinely distinct diagnostics under ADR-3's de-duplication. The
		// expectation is re-derived from the break set the parser counts, which
		// TestEveryLineBreakTheParserCountsIsCountedHere measures.
		{name: "a lone carriage return breaks the line", text: "fi\rrst\n", line: 1, want: "fi"},
		{name: "the line after a lone carriage return", text: "fi\rrst\n", line: 2, want: "rst"},
		{name: "two carriage returns leave an empty line between", text: "a\r\rb\n", line: 2, want: ""},
		{name: "a carriage return after a newline breaks again", text: "a\n\rb\n", line: 3, want: "b"},
		{name: "a carriage return inside a quoted scalar still breaks", text: "a: \"x\ry\"\n", line: 2, want: `y"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := newSource("listeners.yaml", []byte(tc.text))

			if got := src.Line(tc.line); got != tc.want {
				t.Errorf("Line(%d) of %q = %q, want %q", tc.line, tc.text, got, tc.want)
			}
		})
	}
}

// TestEveryLineBreakTheParserCountsIsCountedHere is what makes splitLines' break set a measurement of the
// parser rather than a claim in a comment, and it is the protection that replaced the redactor's lone-return
// fallback when the divergence behind it was fixed at source.
//
// **The invariant, stated as the thing that actually broke:** a diagnostic's line number comes from the
// parser and its line *text* comes from source.Line, so the two must agree about what a line is. They did
// not, for a lone carriage return, and the cost was not a cosmetic slip -- the parser's line exceeded this
// slice, Line answered the empty string, and runeColumn's end-of-line clamp answered the same column for
// every token on that line. Two genuinely different faults then shared (File, Line, Col, Msg) and ADR-3's
// de-duplication merged them into one, which is silent under-reporting on the element class AC #33 rests
// on.
//
// Both directions are asserted per spelling. A separator the parser *does* count must put the marker on a
// later line here too; one it does *not* count must leave the marker on the first line, so a splitLines
// that broke on something the parser treats as content would fail as loudly as one that missed a break.
func TestEveryLineBreakTheParserCountsIsCountedHere(t *testing.T) {
	tests := []struct {
		name      string
		separator string
		// breaksALine is whether the parser counts this separator as a line break. Measured on v1.19.2:
		// the five that do not are refused by the parser outright when used as one, so no document can
		// reach a position question through them.
		breaksALine bool
	}{
		{name: "line feed", separator: "\n", breaksALine: true},
		{name: "carriage return", separator: "\r", breaksALine: true},
		{name: "carriage return line feed", separator: "\r\n", breaksALine: true},
		{name: "two carriage returns", separator: "\r\r", breaksALine: true},
		{name: "line feed then carriage return", separator: "\n\r", breaksALine: true},
		{name: "next line U+0085", separator: "\u0085", breaksALine: false},
		{name: "line separator U+2028", separator: "\u2028", breaksALine: false},
		{name: "paragraph separator U+2029", separator: "\u2029", breaksALine: false},
		{name: "vertical tab", separator: "\v", breaksALine: false},
		{name: "form feed", separator: "\f", breaksALine: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := newSource("listeners.yaml", []byte("a: 1"+tc.separator+"marker: 2\n"))

			_, diags := parseDocument(src)
			if !tc.breaksALine {
				// The parser refuses such a document rather than reading the separator as a break, so
				// there is no position to disagree about. Asserting the refusal is what keeps this row
				// from passing for a separator that silently became a break.
				if len(diags) == 0 {
					t.Fatalf("the parser now accepts %q as a separator; if it counts it as a line break, "+
						"splitLines has to break on it too", tc.separator)
				}
				return
			}
			if len(diags) != 0 {
				t.Fatalf("the parser refuses %q as a separator: %q", tc.separator, messagesOf(diags))
			}

			// The agreement itself: the line the parser puts `marker` on is the line holding its text
			// here. This is the comparison whose failure was the defect.
			_, node := nodeAt(t, "a: 1"+tc.separator+"marker: 2\n", "$.marker")
			at := positionOf(src, node)

			if at.Line() < 2 {
				t.Fatalf("the parser puts `marker` on line %d, so it no longer counts %q as a break; "+
					"splitLines must be re-derived from the set it does count", at.Line(), tc.separator)
			}
			if got := src.Line(at.Line()); got != "marker: 2" {
				t.Errorf("the parser puts `marker` on line %d, where Line answers %q; the two count lines "+
					"differently, so every column on that line collapses and distinct diagnostics merge",
					at.Line(), got)
			}
			// nodeAt selects the value, and `marker: ` is eight runes -- m1 a2 r3 k4 e5 r6 :7 space8 --
			// so the value begins at rune 9. Asserting a column *other than* 1 is what makes this row
			// evidence: 1 is what runeColumn's end-of-line clamp answers for every token on a line whose
			// text is missing, which is precisely the collapse the defect caused.
			if col := at.Col(); col != 9 {
				t.Errorf("the value on `marker`'s line is at column %d, want 9; a column of 1 would mean "+
					"the line text is missing and every token on it collapses to one position", col)
			}
		})
	}
}
