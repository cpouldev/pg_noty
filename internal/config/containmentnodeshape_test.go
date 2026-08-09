package config

import (
	"slices"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// This file generates the containment grid's node-shape dimension: the YAML shapes a value the
// schema declares sensitive can be *written* in, as opposed to the byte classes it can be made of.
//
// It exists because every layout wrote its secret quoted or as a block scalar, so no generated
// document could make a declared-sensitive value parse as a mapping -- and a mapping's own token is
// the colon of its first entry, which redaction was taking for the column its text begins at. The
// first key of such a value was rendered in the clear, inside the very diagnostic that reports the
// mis-shaping. containmentclosure_test.go closed the grid over the fallback's lexical arms and not
// over the path-aware walk's node-shape arms, which is the blind spot that file was written to
// prevent.

// yamlShape names a written shape in the words the walk's three switches tell apart.
type yamlShape string

const (
	shapeScalar        yamlShape = "scalar"
	shapeBlockMapping  yamlShape = "block mapping"
	shapeFlowMapping   yamlShape = "flow mapping"
	shapeBlockSequence yamlShape = "block sequence"
	shapeFlowSequence  yamlShape = "flow sequence"
)

// writtenValueShape is one shape a sensitive value can be written in: how to write it, and what the
// parser must then read. Both positions are declared, because two different switches read them --
// container is what valuesOf splits and containerEndLine answers for, located is what valuesOf hands
// to locate and writtenStartOf branches on.
//
// startsAtColumn is the rune column the first node handed to locate begins at, counted from the row's
// own source line rather than recorded from a run, so a derivation that drifts fails rather than
// pinning its own answer. Every row writes ` secrets:` -- six spaces, `secrets` at columns 7 to 13,
// the colon at 14 -- and plants `vPGNOTY-SHAPE-PROBE`.
type writtenValueShape struct {
	name           string
	container      yamlShape
	located        yamlShape
	startsAtColumn columnInRunes
	write          func(secret string) string
}

// secretsLocator is where these rows write their sensitive value: signing.secrets, which the schema
// declares sensitive in full. That is the strongest declaration in the table, so a surviving marker
// is a secret by the table's own account rather than by the grid's.
const secretsLocator = "$.listeners[0].destination.signing.secrets"

// plainly and insideAFlowContainer write a secret into a row, raw in both cases.
//
// They were a head byte and a quoting, added because a generated `#` at the head of a value turned
// its line into a comment and a generated `]` closed a flow container early: the parser then placed
// the markers outside every declared-sensitive value and the row reported a leak the redactor had
// not committed. Both were accurate about the byte class they named and far broader than it -- one
// byte class needed excluding and every byte class was excluded, including the `&`, `*` and `!`
// writtenStartOf documents itself as branching on, so a documented branch was unreachable from the
// family written to cover it.
//
// The fix is in the oracle instead: assertPlantedSecretsAreContained now reads which bytes the
// document actually makes sensitive whenever it parses, so a marker a generated tail moved out of a
// value is no longer required to be absent. The payload is free to hold every byte again. They stay
// as named functions because each still says which position it writes.
func plainly(secret string) string {
	return secret
}

func insideAFlowContainer(secret string) string {
	return secret
}

// The mapping rows are written unquoted, because writing a secret unquoted is exactly what makes a
// parser read it as a mapping and is what kept that shape out of the grid.
var writtenValueShapes = slices.Concat(oneLineValueShapes, multiLineValueShapes)

// oneLineValueShapes is one row per shape the path-aware walk's own switches branch on, each
// written as that shape's smallest instance. multiLineValueShapes writes the same shapes large,
// which is the dimension the reach walk branches on
// (containmentnodeextent_test.go).
var oneLineValueShapes = []writtenValueShape{
	{
		name: "a plain scalar", container: shapeScalar, located: shapeScalar,
		// The blank after the colon is column 15, so the scalar begins at 16.
		startsAtColumn: 16,
		write:          func(secret string) string { return "      secrets: " + plainly(secret) + "\n" },
	},
	{
		name: "a block sequence of plain scalars", container: shapeBlockSequence, located: shapeScalar,
		// `      - ` is eight runes, so the element begins at 9.
		startsAtColumn: 9,
		write: func(secret string) string {
			return "      secrets:\n      - " + plainly(secret) + "\n"
		},
	},
	{
		name: "a flow sequence of quoted scalars", container: shapeFlowSequence, located: shapeScalar,
		// The opening bracket is column 16, so the quoted element begins at its own quote, 17.
		startsAtColumn: 17,
		write: func(secret string) string {
			return "      secrets: [" + insideAFlowContainer(secret) + "]\n"
		},
	},
	{
		name: "a block mapping", container: shapeBlockMapping, located: shapeBlockMapping,
		// Eight columns of indentation, so the mapping's line begins at 9 -- which is its first key
		// here, and is the answer whether or not it has one.
		startsAtColumn: 9,
		write: func(secret string) string {
			return "      secrets:\n        " + plainly(secret) + ": " + plainly(secret) + "\n"
		},
	},
	{
		name:      "a block mapping written as a sequence item",
		container: shapeBlockSequence, located: shapeBlockMapping,
		// Six columns of indentation, so the line begins at its `-`, 7: two runes before the first
		// key, which is the over-redaction answering from the line costs and D3 prefers to a guess.
		startsAtColumn: 7,
		write: func(secret string) string {
			return "      secrets:\n      - " + plainly(secret) + ": " + plainly(secret) + "\n"
		},
	},
	{
		name: "a flow mapping of quoted scalars", container: shapeFlowMapping, located: shapeFlowMapping,
		// A flow mapping's own token is its brace, at column 16.
		startsAtColumn: 16,
		write: func(secret string) string {
			quoted := insideAFlowContainer(secret)
			return "      secrets: {" + quoted + ": " + quoted + "}\n"
		},
	},
}

// writtenValueShapeLayouts plant a generated secret in each shape, folded onto one line so that a
// generated break cannot move half of it out of the shape the row is named for.
func writtenValueShapeLayouts() []secretLayout {
	layouts := make([]secretLayout, 0, len(writtenValueShapes))
	for _, shape := range writtenValueShapes {
		layouts = append(layouts, secretLayout{
			name: "a secret written as " + shape.name,
			body: func(secret string, key keySpelling) string {
				return signingSecrets(shape.write(onOneLine(secret)))
			},
		})
	}
	return layouts
}

// shapeProbeSecret is what the closure tests plant instead of a generated secret: they measure the
// shape a row produces, so the payload must be the one thing that cannot alter it.
const shapeProbeSecret = "PGNOTY-SHAPE-PROBE"

// sensitiveValueWrittenAs parses one row's document and answers the source together with the node
// the sensitive key holds. It reads through the package's own parse-and-select helpers rather than
// repeating either.
func sensitiveValueWrittenAs(t *testing.T, shape writtenValueShape) (*source, ast.Node) {
	t.Helper()

	document := "version: 1\n" + signingSecrets(shape.write(shapeProbeSecret))
	return nodeAt(t, document, secretsLocator)
}

// shapeOf names a node's written shape as writtenStartOf sees it -- without reading through node
// properties, which that function deliberately does not either. No row writes an anchor or a tag, so
// this agrees with the peeled view valuesOf and containerEndLine take.
func shapeOf(node ast.Node) yamlShape {
	switch held := node.(type) {
	case *ast.MappingNode:
		if held.IsFlowStyle {
			return shapeFlowMapping
		}
		return shapeBlockMapping
	case *ast.SequenceNode:
		if held.IsFlowStyle {
			return shapeFlowSequence
		}
		return shapeBlockSequence
	default:
		return shapeScalar
	}
}
