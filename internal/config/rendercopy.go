package config

import "slices"

// renderCopy is the mutable render-only copy of source text. The struct prevents
// source.lines from being passed in directly, and its sole constructor clones both
// slices so redaction cannot mutate bytes later quoted as the original source
// (ADR-5, TestARenderCopyDoesNotAliasItsSource).
type renderCopy struct {
	lines     []string
	continues []bool
	shifts    [][]columnShift
}

func newRenderCopy(text *source) renderCopy {
	return renderCopy{
		lines:     slices.Clone(text.lines),
		continues: slices.Clone(text.continuesPhysicalLine),
		shifts:    make([][]columnShift, len(text.lines)),
	}
}

func (c renderCopy) continuesTheLineAbove(number int) bool {
	return continuesTheLineAbove(c.continues, number)
}

// line and rewrite take 1-based line numbers, matching every source position in
// the package.
func (c renderCopy) line(number int) string { return c.lines[number-1] }

func (c renderCopy) rewrite(number int, text string) { c.lines[number-1] = text }

func (c renderCopy) count() int { return len(c.lines) }

// carryReplacement records the piecewise mapping across one replacement. Source positions
// inside removed text deterministically point at the replacement's first rune; positions at or
// after its end carry the length delta. The original coordinates stay the reference even though
// replacements are applied right to left.
func (c renderCopy) carryReplacement(
	number int,
	from, past columnInRunes,
	replacementRunes int,
) {
	if past < from {
		return
	}
	c.shifts[number-1] = append(c.shifts[number-1], columnShift{
		from: from,
		past: past,
		by:   replacementRunes - int(past-from),
	})
}

func (c renderCopy) quotable() quotableLines {
	return quotableLines{text: c.lines, shifts: c.shifts}
}

// quotableLines is source text after structural redaction, together with the only coordinate
// transform redaction can require: length-changing replacements that leave text to their right.
// The renderer can read it but has no rewrite operation.
type quotableLines struct {
	text   []string
	shifts [][]columnShift

	// withheld is every word redaction removed from the source, which is what a *second* channel of
	// rendered text has to be checked against. A diagnostic's message and hint reach the page beside
	// the quoted lines without being quoted from them -- the parser composes its own out of the
	// document, so `mapping key "S3CRET-KEY" already defined` rendered bytes the line above it had
	// already been blanked of (redact.go's withheldWords).
	withheld []string
}

// withholding is these lines together with the words redaction removed to produce them.
func (q quotableLines) withholding(words []string) quotableLines {
	q.withheld = words
	return q
}

type columnShift struct {
	from columnInRunes
	past columnInRunes
	by   int
}

func newQuotableLines(lines []string) quotableLines {
	return quotableLines{text: lines, shifts: make([][]columnShift, len(lines))}
}

// column carries an original source column onto the corresponding rendered line.
func (q quotableLines) column(number, original int) int {
	if number < firstLine || number > len(q.shifts) {
		return original
	}

	adjusted := original
	for _, shift := range q.shifts[number-1] {
		source := columnInRunes(original)
		switch {
		case source >= shift.past:
			adjusted += shift.by
		case source >= shift.from:
			adjusted += int(shift.from - source)
		}
	}
	return max(firstColumn, adjusted)
}
