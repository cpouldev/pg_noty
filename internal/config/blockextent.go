package config

// This file holds the package's one reading of YAML's block rule: how far down a document a value
// written without a closing delimiter reaches.
//
// It is a file of its own because three positions need that answer and a fourth used to invent one.
// A block container writes no closer, and containerEndLine quite correctly reports that
// (sensitiveextent.go). "Reports no closer" and "has no delimiter" are different facts, though:
// indentation is what delimits a block, and a bound derived from the container's *members* stops at
// the last member -- so a comment an author wrote after the last item, inside a value the table
// declares secret in full, fell outside a scan that read as if it covered the whole container.

// lastLineOfTheValueAt is the last line the value beginning on line occupies, read from the raw
// document by YAML's own block rule and from nothing any caller computes.
//
// How far "inside" reaches depends on whether the value has a line of its own, which is the same
// distinction the reach walk reads and is therefore taken from the same function
// (leastIndentInsideTheValue). A block container's first line is a line of its own text and its
// sibling entries sit at exactly that indentation, so a later line at the same column is still
// inside it -- which is what puts a comment written between or after the items of a block sequence
// inside the sequence. A value written beside its key shares that line with the key, whose
// indentation is the line's own, so only a line indented past it is inside.
//
// A blank line is skipped rather than ending the value, because its own bytes place it nowhere and
// a block scalar keeps it.
func lastLineOfTheValueAt(text *source, line int, ownLine bool) int {
	least := leastIndentInsideTheValue(text.Line(line), ownLine)

	last := line
	for number := line + 1; number <= len(text.lines); number++ {
		written := text.Line(number)
		if isBlankLine(written) {
			continue
		}
		if indentWidth(written) < least {
			return last
		}
		last = number
	}
	return last
}
