package config

import (
	"os"
	"slices"
	"strings"
)

// source is the configuration text, held immutably for the lifetime of one run.
//
// Immutability is a security property, not a convenience: interpolation substitutes
// resolved values into parsed nodes, never into these bytes, and every rendered
// snippet is read from here. A resolved secret therefore cannot reach rendered
// output, because the only text a renderer can quote is the text that was on disk.
type source struct {
	name  string
	bytes []byte
	lines []string
	// continuesPhysicalLine[i] reports whether line i+1 was produced by splitting a lone carriage
	// return rather than a line feed. Such a line is a new line to the *parser* and not to the person
	// who wrote the file: their editor shows one physical line, and the bytes after the return sit on
	// it. Redaction needs that distinction and positions must not have it (redactionScope below).
	continuesPhysicalLine []bool
}

// newSource holds data as the immutable source named name. The bytes are copied, so
// a caller that reuses or mutates its buffer cannot change what is reported later.
func newSource(name string, data []byte) *source {
	frozen := slices.Clone(data)

	return &source{
		name:  name,
		bytes: frozen,
		// Split once. Positions are resolved against these lines by line number,
		// never by byte offset, because the parser's offsets drift across comments
		// and block scalars (V3).
		lines:                 splitLines(string(frozen)),
		continuesPhysicalLine: carriageReturnSplits(string(frozen)),
	}
}

// carriageReturnSplits marks the lines splitLines produced by breaking a lone carriage return, one
// entry per line and aligned with them.
//
// **Why the distinction is kept rather than resolved one way.** The two consumers need opposite
// answers, and collapsing them is what produced a secret leak. A *position* must count a lone return
// as a break, because the parser does, and a line numbering that disagrees with the parser's collapses
// every column on the affected line (Implementation Note 5). *Redaction* must not, because a lone
// return is a control character inside the line a human wrote: their editor shows one line, the bytes
// after the return are on it, and a redactor that treats them as a fresh line stops covering them --
// which rendered a planted secret verbatim. Measured: the parser ends a block scalar at a lone return
// exactly as it would at a dedent, so those bytes parse as a key of their own and no sensitive name
// governs them (TestGoccyEndsABlockScalarAtALoneCarriageReturn).
//
// The first line continues nothing, so its entry is always false.
func carriageReturnSplits(text string) []bool {
	unified := strings.ReplaceAll(text, "\r\n", "\n")

	splits := []bool{false}
	for _, char := range unified {
		if char == '\n' {
			splits = append(splits, false)
			continue
		}
		if char == '\r' {
			splits = append(splits, true)
		}
	}
	return splits
}

// splitLines is the raw text as a renderer quotes it and as a column is counted over it: one entry per
// line, with no line break left on the end of any of them. Keeping one would put an invisible character
// into every rendered snippet, and would move the caret of a token anchored past its line's end by one
// (Implementation Note 12 records the measurement).
//
// **What a line break is here is the parser's definition, not LF alone, and that is a correctness
// requirement rather than a tidiness one.** Every line number a diagnostic carries is the parser's, and
// every line of *text* is one of these -- so if the two count lines differently, then for any document
// after the first disagreement `Line(n)` answers with a different line than the one the number names.
// YAML 1.2 §5.4 makes CR, LF and CRLF all line breaks and the parser follows it; splitting on LF alone
// left a lone CR as content, so `a: 1\rb: 2` was one line here and two to the parser. The cost was not a
// wrong snippet but silent under-reporting: the parser's line then exceeded this slice, `Line` answered
// the empty string, and runeColumn's end-of-line clamp collapsed **every** column on that line to 1 --
// so two genuinely different faults became one `(File, Line, Col, Msg)` and ADR-3's de-duplication
// merged them. TestEveryLineBreakTheParserCountsIsCountedHere pins the set against the parser rather than
// against this comment.
//
// Measured on v1.19.2: CR, LF and CRLF break a line, and NEL, LS, PS, vertical tab and form feed do not
// -- the parser refuses a document using one as a separator, so none of them can reach a position
// question. Normalising the two-character break first is what keeps CRLF one break rather than two.
//
// The bytes above keep the file exactly as it was read, line breaks included, because they are what the
// parser is given; these lines are what a human is shown.
func splitLines(text string) []string {
	unified := strings.ReplaceAll(text, "\r\n", "\n")
	unified = strings.ReplaceAll(unified, "\r", "\n")

	return strings.Split(unified, "\n")
}

// readSource reads the configuration file at path. This is the only file the
// package ever opens.
func readSource(path string) (*source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return newSource(path, data), nil
}

// Name is the display name used in diagnostics.
func (s *source) Name() string { return s.name }

// CloneBytes is a fresh copy of the configuration text as read. The name says so
// because the whole file is copied on every call: a caller that wants one line calls
// Line instead, and the parse path takes this once per load.
//
// It is a copy because the immutability above is a security property: handing out the
// underlying array would let any holder write through it, leaving "the text a renderer
// quotes is the text that was on disk" a convention rather than a structural fact.
func (s *source) CloneBytes() []byte { return slices.Clone(s.bytes) }

// Line returns the raw text of the given 1-based line, or the empty string when the
// line is outside the source. Out-of-range access is normal rather than
// exceptional: a diagnostic about the file as a whole carries no line number.
func (s *source) Line(number int) string {
	if number < 1 || number > len(s.lines) {
		return ""
	}
	return s.lines[number-1]
}

// continuesTheLineAbove reports whether the given 1-based line is the tail of the physical line above it,
// split off by a lone carriage return.
//
// **One spelling, package-wide, and that is a safety property rather than tidiness.** Three copies of this
// question existed for a while -- one here, one on renderCopy, and one written inline against a 0-based
// index -- inside the mechanism where a one-position shift renders a secret. They had already diverged in
// index convention. Everything that asks now asks here, so a caller cannot pick the wrong convention.
//
// Out of range answers false, which is the safe direction: a line that does not exist continues nothing, so
// redaction decides it on its own bytes rather than inheriting cover. That the range can never be exceeded
// for a real line is TestTheLineMarkingIsAlignedWithTheLines' claim, so this is a bound on a pinned
// invariant rather than a hedge against one.
func continuesTheLineAbove(marks []bool, number int) bool {
	if number < 1 || number > len(marks) {
		return false
	}
	return marks[number-1]
}

// continuesTheLineAbove is the source's own marking, asked through the one helper.
func (s *source) continuesTheLineAbove(number int) bool {
	return continuesTheLineAbove(s.continuesPhysicalLine, number)
}
