package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// This file is the node-shape dimension's second half: the same shapes, written large.
//
// Every row in containmentnodeshape_test.go writes its shape's *minimal* instance -- one entry, one
// line -- and the closure test beside them certified each arm as covered on that basis. But the
// reach machinery does not branch on the shape; it branches on how many lines a value occupies and
// how many entries it holds. Holding both at one meant every arm was entered and no arm's own
// behaviour was ever exercised, which is why 2.7 million generated executions passed while a
// two-entry block mapping rendered every entry after its first.
//
// A row per arm is not coverage of the arm. After closing a generated dimension over the code's
// arms, ask what each arm branches on *besides* the arm, and write a row that is non-degenerate in
// it.

// distinctly writes a secret with a leading letter that makes two occurrences of it different keys.
// A mapping needs distinct keys or the document is a duplicate-key error, and this row would then
// measure the fallback while claiming to measure both branches.
func distinctly(mark, secret string) string {
	return mark + plainly(secret)
}

// multiLineValueShapes is one row per container shape the walk branches on, each written across more
// than one line and holding more than one entry. The secret is planted on every line of every row,
// so a reach that stops early leaks a marker rather than merely under-blanking whitespace.
var multiLineValueShapes = []writtenValueShape{
	{
		name: "a block mapping of two entries", container: shapeBlockMapping, located: shapeBlockMapping,
		// Eight columns of indentation, so the mapping's first line begins at 9.
		startsAtColumn: 9,
		write: func(secret string) string {
			return "      secrets:\n" +
				"        " + distinctly("a", secret) + ": " + plainly(secret) + "\n" +
				"        " + distinctly("b", secret) + ": " + plainly(secret) + "\n"
		},
	},
	{
		name:      "a block mapping of two entries written as a sequence item",
		container: shapeBlockSequence, located: shapeBlockMapping,
		// Six columns of indentation, so the line begins at its `-`, 7: the two runes of the item
		// marker are the over-redaction answering from the line costs.
		startsAtColumn: 7,
		write: func(secret string) string {
			return "      secrets:\n" +
				"      - " + distinctly("a", secret) + ": " + plainly(secret) + "\n" +
				"        " + distinctly("b", secret) + ": " + plainly(secret) + "\n"
		},
	},
	{
		name: "a block sequence of two plain scalars", container: shapeBlockSequence, located: shapeScalar,
		// `      - ` is eight runes, so the first element begins at 9.
		startsAtColumn: 9,
		write: func(secret string) string {
			return "      secrets:\n      - " + plainly(secret) + "\n      - " + plainly(secret) + "\n"
		},
	},
	{
		name: "a plain scalar written across two lines", container: shapeScalar, located: shapeScalar,
		// `      secrets: ` is fifteen runes, so the scalar begins at 16.
		startsAtColumn: 16,
		write: func(secret string) string {
			return "      secrets: " + plainly(secret) + "\n        " + plainly(secret) + "\n"
		},
	},
	{
		name: "a flow sequence written across two lines", container: shapeFlowSequence, located: shapeScalar,
		// The opening bracket is column 16, so the first quoted element begins at its own quote, 17.
		startsAtColumn: 17,
		write: func(secret string) string {
			quoted := insideAFlowContainer(secret)
			return "      secrets: [" + quoted + ",\n        " + quoted + "]\n"
		},
	},
	{
		name: "a flow mapping written across two lines", container: shapeFlowMapping, located: shapeFlowMapping,
		// A flow mapping's own token is its brace, at column 16.
		startsAtColumn: 16,
		write: func(secret string) string {
			quoted := insideAFlowContainer(secret)
			return "      secrets: {" + insideAFlowContainer(distinctly("a", secret)) + ": " + quoted +
				",\n        " + insideAFlowContainer(distinctly("b", secret)) + ": " + quoted + "}\n"
		},
	},
}

// TestEveryShapeTheWalkBranchesOnIsAlsoWrittenLarge closes the node-shape dimension over the
// dimension the *reach* code branches on. Both measurements come from parsing each row rather than
// from its name, so a row that quietly folded back onto one line fails here.
func TestEveryShapeTheWalkBranchesOnIsAlsoWrittenLarge(t *testing.T) {
	const wantLines, wantEntries = 2, 2

	largest := map[yamlShape]struct{ lines, entries int }{}
	for _, shape := range writtenValueShapes {
		t.Run(shape.name, func(t *testing.T) {
			_, value := sensitiveValueWrittenAs(t, shape)

			held := largest[shape.container]
			largest[shape.container] = struct{ lines, entries int }{
				lines:   max(held.lines, linesWrittenBy(shape)),
				entries: max(held.entries, entriesHeldBy(value)),
			}
		})
	}

	for _, shape := range containerShapesTheWalkBranchesOn {
		written := largest[shape]
		if written.lines < wantLines {
			t.Errorf("every row writing %q occupies %d line; the reach walk branches on how many "+
				"lines a value occupies, so its own behaviour is never entered", shape, written.lines)
		}
		if shape != shapeScalar && written.entries < wantEntries {
			t.Errorf("every row writing %q holds %d entry; the reach walk stopped at the second "+
				"entry of exactly such a value, and no row could reach it", shape, written.entries)
		}
	}
}

// linesWrittenBy is how many lines one row's declaration occupies, measured through the package's
// own break set so a row written with any of them is counted the way the parser counts it.
func linesWrittenBy(shape writtenValueShape) int {
	written := shape.write(onOneLine(secretMarkedOnEveryLine("plain").text))

	return len(linesWithTheirBreaks(strings.TrimSuffix(written, "\n")))
}

// entriesHeldBy is how many entries a parsed value holds: its elements when it is a sequence, its
// mapping entries when it is a mapping, and one when it is neither.
func entriesHeldBy(value ast.Node) int {
	switch held := beneathNodeProperties(value).(type) {
	case *ast.MappingNode:
		return len(held.Values)
	case *ast.SequenceNode:
		return len(held.Values)
	default:
		return 1
	}
}
