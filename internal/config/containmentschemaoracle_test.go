package config

import (
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// This file is the containment oracle's authority, and it is deliberately not the walk whose
// output the containment subjects judge.
//
// The oracle used to answer "is this marker inside a value the schema declares sensitive" by
// calling sensitiveValues. A byte that walk failed to attribute was therefore not sensitive *by
// construction*: on a document holding an anchor name between two elements of a declared secret,
// the walk reported two values neither of which held the name, the oracle answered false, and
// assertNoMarkerInsideASensitiveValueSurvives returned while the renderer was quoting it. Deleting
// the oracle's line clause left the whole suite green, which is what a tautology looks like from
// the inside.
//
// The authority here is schema.go's declarations and the parser's own paths, composed without the
// walk: every locator the table declares sensitive is expanded into the concrete paths this
// document writes, and the node the parser puts at each is asked for its text and its extent.
// Nothing the schema walk decides can widen or narrow that answer, so a walk that drops a subtree
// is caught by the oracle rather than excused by it. Both halves of that independence are
// asserted, structurally by TestTheContainmentOracleNeverAsksTheWalkItAudits and behaviourally by
// TestTheContainmentOracleClaimsBytesTheWalkDoesNotAttribute.

// declaredSensitiveExtent is one region of a document the table declares sensitive: the concrete
// locator that declared it, the text the parser put there, and the span of raw source it occupies
// from (line, from) up to but not including (through, past).
//
// The span is raw source rather than the node's own text because the two differ exactly where it
// matters. `node.String()` renders a flow sequence back as `[FIRST, SECOND]` -- the anchor name an
// author wrote between the elements is a property of the tree, not of the text, so a container's
// rendering is blind to the one class of byte that reaches rendered output without belonging to any
// child (containerinterior.go). An oracle built on the rendering inherits that blindness.
type declaredSensitiveExtent struct {
	locator string
	line    int
	from    columnInRunes
	through int
	past    columnInRunes
	text    string
}

// sensitiveSchemaLocators is every locator schema.go declares a sensitive extent for, read through
// declaredSchemaPositions so that a key given a sensitivity in the table joins this oracle by being
// declared rather than by being remembered.
func sensitiveSchemaLocators() []string {
	var found []string
	for _, at := range declaredSchemaPositions() {
		if at.sensitive != publicValue {
			found = append(found, at.locator)
		}
	}
	return found
}

// declaredSensitiveExtentsOf is every region of one parsed document the table declares sensitive.
func declaredSensitiveExtentsOf(text *source, root ast.Node) []declaredSensitiveExtent {
	var found []declaredSensitiveExtent

	for _, locator := range sensitiveSchemaLocators() {
		for _, written := range concreteLocatorsOf(root, locator) {
			node, exists := nodeWrittenAt(root, written)
			if !exists {
				continue
			}
			// The package's one column derivation, which ADR-4 requires of every position: what is
			// re-derived here is which values are sensitive, never where a value sits. The extent
			// reaches back to the block the value's key opened, exactly as the redactor's does, so
			// the two agree about the runes an author wrote between the colon and the value.
			start := writtenStartReachingItsBlockOpener(text, node)
			if start.line < firstLine || start.line > len(text.lines) {
				continue
			}

			// The downward bound is taken from the value's own first line, never from the gap's:
			// a note may be indented less than the value it sits above, and reading the block rule
			// from it would stretch the extent over the value's siblings and report a leak the
			// redactor did not commit.
			through := lastLineOfTheValueAt(text, start.line, start.beginsOnALineOfItsOwn)
			through = max(through, closingDelimiterLineOf(text, node))
			from, line := start.column, start.line
			if start.reachesBackTo < start.line {
				from, line = firstColumn+columnInRunes(indentWidth(text.Line(start.reachesBackTo))),
					start.reachesBackTo
			}
			found = append(found, declaredSensitiveExtent{
				locator: written,
				line:    line,
				from:    from,
				through: through,
				past:    pastTheEndOf(text, node, through),
				text:    node.String(),
			})
		}
	}
	return found
}

// closingDelimiterLineOf is the line a flow container's closer sits on, and noClosingIndicator for
// everything else, so a flow container written across lines reaches its closer even where the
// indentation rule would have stopped short of it.
func closingDelimiterLineOf(text *source, node ast.Node) int {
	closer := closingDelimiterOf(node)
	if closer == nil {
		return noClosingIndicator
	}
	return tokenPosition(text, closer, pathOf(node)).Line()
}

// pastTheEndOf is the rune column the value stops before on its last line.
//
// A flow container writes a closing delimiter and the parser reports it, so the extent stops just
// past that delimiter: a comment written beyond it -- `secrets: [A]#B`, which a generated tail
// produces by closing the container early -- is then outside the value, which is what keeps this
// oracle from reporting a leak the redactor did not commit. Everything else runs to the end of its
// last line, because a block container closes with nothing and a scalar owns what follows it.
func pastTheEndOf(text *source, node ast.Node, through int) columnInRunes {
	endOfLine := firstColumn + columnInRunes(runeCount(text.Line(through)))

	closer := closingDelimiterOf(node)
	if closer == nil {
		return endOfLine
	}
	at := tokenPosition(text, closer, pathOf(node))
	if at.Line() != through {
		return endOfLine
	}
	return min(columnInRunes(at.Col())+1, endOfLine)
}

// closingDelimiterOf is the closer the parser reports for a flow container, and nil for a block one
// or a scalar. It reads the same two fields containerEndLine reads, for the same reason and with the
// same answer for each shape; what this position needs in addition is the closer's column, which
// that function's int result cannot carry.
func closingDelimiterOf(node ast.Node) *token.Token {
	switch held := beneathNodeProperties(node).(type) {
	case *ast.MappingNode:
		return held.End
	case *ast.SequenceNode:
		return held.End
	default:
		return nil
	}
}

// The block rule this oracle reads to bound a value that writes no closer is blockextent.go's
// lastLineOfTheValueAt. It used to be written here, where the redactor could not reach it, and the
// redactor's own interior scan therefore bounded a block container by its last child instead. One
// derivation now answers both.
