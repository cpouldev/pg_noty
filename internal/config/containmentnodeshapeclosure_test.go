package config

import (
	"slices"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// This file reconciles the node-shape rows against the arms of the path-aware walk they exist to
// reach, and pins the one behavioural difference the leak turned on.

// containerShapesTheWalkBranchesOn are the shapes a sensitive key's own value can take: what
// valuesOf either splits or passes through, and what containerEndLine answers for.
//
// locatedShapesTheWalkBranchesOn are the shapes valuesOf can then hand to locate. No sequence is
// among them, because valuesOf splits every sequence into its elements -- which is why the two
// positions are reconciled against two sets rather than one.
var (
	containerShapesTheWalkBranchesOn = []yamlShape{
		shapeScalar, shapeBlockMapping, shapeFlowMapping, shapeBlockSequence, shapeFlowSequence,
	}
	locatedShapesTheWalkBranchesOn = []yamlShape{shapeScalar, shapeBlockMapping, shapeFlowMapping}
)

// TestEveryWrittenValueShapeParsesAsTheShapeItIsNamedFor measures each row against the parser rather
// than trusting its name, and then reconciles the shapes observed with every arm the walk has. A row
// that stopped producing its shape would otherwise leave those arms uncovered while the grid still
// reported six layouts.
func TestEveryWrittenValueShapeParsesAsTheShapeItIsNamedFor(t *testing.T) {
	containers := map[yamlShape]bool{}
	located := map[yamlShape]bool{}

	for _, shape := range writtenValueShapes {
		t.Run(shape.name, func(t *testing.T) {
			_, value := sensitiveValueWrittenAs(t, shape)

			if got := shapeOf(value); got != shape.container {
				t.Fatalf("the sensitive key's value parses as %q, and the row is named for %q",
					got, shape.container)
			}
			for _, child := range valuesOf(value) {
				if got := shapeOf(child); got != shape.located {
					t.Errorf("valuesOf hands locate a %q, and the row is named for %q",
						got, shape.located)
				}
			}
		})
		containers[shape.container] = true
		located[shape.located] = true
	}

	assertEveryShapeIsWritten(t, "the sensitive key's own value",
		containerShapesTheWalkBranchesOn, containers)
	assertEveryShapeIsWritten(t, "the node valuesOf hands to locate",
		locatedShapesTheWalkBranchesOn, located)
}

// assertEveryShapeIsWritten reports the shapes a position must be written in and is not, so a shape
// added to yamlShape without a row fails here by name instead of leaving an arm silently unreached.
func assertEveryShapeIsWritten(t *testing.T, position string, wanted []yamlShape, seen map[yamlShape]bool) {
	t.Helper()

	for _, shape := range wanted {
		if !seen[shape] {
			t.Errorf("no row writes %s as %q, so the arm reading one is unreachable from every "+
				"generated document", position, shape)
		}
	}
}

// TestEveryNodeShapeRowHasALayout keeps the family and its table the same size, so a shape added to
// the table without reaching the grid fails here rather than narrowing every property quantified
// over it.
func TestEveryNodeShapeRowHasALayout(t *testing.T) {
	if got := len(writtenValueShapeLayouts()); got != len(writtenValueShapes) {
		t.Errorf("%d node-shape layouts for %d rows", got, len(writtenValueShapes))
	}
}

// TestWrittenStartOfSeparatesABlockMappingFromEveryOtherShape pins the answer redaction needs
// against the answer a caret needs, over every shape the grid writes. Two claims, because either
// alone is weak: the column is the one each row counted from its own source line, and the two
// derivations *disagree* exactly for a block mapping -- so a rewrite collapsing them back into one
// fails here rather than passing.
func TestWrittenStartOfSeparatesABlockMappingFromEveryOtherShape(t *testing.T) {
	for _, shape := range writtenValueShapes {
		t.Run(shape.name, func(t *testing.T) {
			text, value := sensitiveValueWrittenAs(t, shape)
			held := valuesOf(value)

			if written := writtenStartOf(text, held[0]).column; written != shape.startsAtColumn {
				t.Errorf("writtenStartOf answers column %d, and the row's own line puts the value's "+
					"text at %d", written, shape.startsAtColumn)
			}

			for _, node := range held {
				caret := columnInRunes(positionOf(text, node).Col())
				at := writtenStartOf(text, node).column

				if differ := caret != at; differ != (shapeOf(node) == shapeBlockMapping) {
					t.Errorf("writtenStartOf answers column %d and positionOf %d for %q; only a block "+
						"mapping's own token sits inside the text it begins", at, caret, shapeOf(node))
				}
			}
		})
	}
}

// blockStyleShapes are the shapes YAML writes without delimiters, so their text begins on a line of
// its own beneath their key. They are the shapes whose sibling entries sit at the anchor line's own
// indentation, which is the distinction the reach walk reads.
var blockStyleShapes = []yamlShape{shapeBlockMapping, shapeBlockSequence}

// TestWrittenStartOfReportsWhetherAValueIsAnchoredInsideItself pins the second answer, per shape,
// against a rule derived from the shape rather than from a run. It is the *reach* half of the same
// question the test above pins the column half of: both readings were derived from one another for a
// round, and a block mapping's redaction stopped at its second entry.
func TestWrittenStartOfReportsWhetherAValueIsAnchoredInsideItself(t *testing.T) {
	seen := map[yamlShape]bool{}

	for _, shape := range writtenValueShapes {
		t.Run(shape.name, func(t *testing.T) {
			text, value := sensitiveValueWrittenAs(t, shape)

			for _, node := range append([]ast.Node{value}, valuesOf(value)...) {
				written := slices.Contains(blockStyleShapes, shapeOf(node))
				if got := writtenStartOf(text, node).beginsOnALineOfItsOwn; got != written {
					t.Errorf("writtenStartOf reports beginsOnALineOfItsOwn=%v for a %q; a container "+
						"YAML writes without delimiters is anchored on a line of its own text, and "+
						"every other shape on the line its key is written on", got, shapeOf(node))
				}
			}
			seen[shape.container] = true
		})
	}

	for _, shape := range blockStyleShapes {
		if !seen[shape] {
			t.Errorf("no row writes a %q, so the reading this test separates is asserted for one "+
				"side of the distinction only", shape)
		}
	}
}
