package config

import (
	"strings"
	"testing"
)

// This file derives, from schema.go's own table, every position a value can be written at, and
// writes a document that puts one there.
//
// Two subjects read it. The fail-closed cases (schemapositionsfailclosed_test.go) plant a container
// at each, which is the shape the walk's three name-shaped arms used to answer "nothing here is
// sensitive" for. The containment grid's schema-position dimension
// (containmentschemapositions_test.go) plants a generated secret at one row per arm, because a
// generator that varies value layout and key spelling while planting at four fixed positions reads
// as universal and excludes the dimension the walk actually branches on.

// freeFormProbeName is a name written at a level that declares none. It is spelled as an HTTP field
// name because that is what the one free-form level in the table holds.
const freeFormProbeName = "X-Probe"

// schemaStep is one key on the way to a position, together with whether the value beneath it is
// written as a sequence of the shape the next step lives in.
type schemaStep struct {
	name       string
	inSequence bool
}

// schemaPosition is one place the contract lets a value be written, and what the table says there.
type schemaPosition struct {
	steps []schemaStep
	// locator is the goccy path of the value written here, with sequence indices collapsed, so it
	// can be compared with the path the parser reports for a node.
	locator string
	// sensitive is the diagnostic-render extent the table declares for this key, and child the
	// shape it says is written beneath it. A key naming no child describes a scalar leaf.
	sensitive sensitivity
	child     levelName
}

// declaredSchemaPositions is every position reachable from the root of the table.
//
// The table is a directed acyclic graph -- `retry` and `headers` are each reached from two parents
// and neither reaches an ancestor -- which TestTheDeclaredPositionsCoverTheWholeTable pins by count
// so that a level made cyclic fails there rather than by not returning.
func declaredSchemaPositions() []schemaPosition {
	var found []schemaPosition

	var walk func(name levelName, prefix string, steps []schemaStep)
	walk = func(name levelName, prefix string, steps []schemaStep) {
		level := schemaLevels[name]
		if level.freeForm {
			// A name, not a key spec: composing one here would be a second statement of the
			// contract's shape, which TestTheShapeIsDeclaredInTheTableAndNowhereElse forbids.
			found = append(found, positionAt(prefix, steps, freeFormProbeName, publicValue))
			return
		}
		for _, spec := range level.keys {
			at := positionAt(prefix, steps, spec.name, spec.sensitive)
			at.child = spec.child
			found = append(found, at)
			if _, declared := schemaLevels[spec.child]; declared {
				walk(spec.child, at.beneath(), at.steps)
			}
		}
	}

	walk(levelRoot, "$", nil)
	return found
}

// positionAt is one position, named rather than specified: the free-form call site has no key spec
// to hand over, because the level it walks declares no keys at all. Composing one there would be a
// second statement of the contract's shape, which TestTheShapeIsDeclaredInTheTableAndNowhereElse
// forbids, so what a caller knows about the position is passed as the one fact it has.
//
// Whether the position sits under a free-form level was carried on the result too, and read by
// nothing. A field no consumer asks for is a claim no test can hold to anything, so it is gone
// rather than left looking like state.
func positionAt(
	prefix string,
	steps []schemaStep,
	name string,
	sensitive sensitivity,
) schemaPosition {
	written := make([]schemaStep, len(steps), len(steps)+1)
	copy(written, steps)

	return schemaPosition{
		steps:     append(written, schemaStep{name: name}),
		locator:   prefix + "." + name,
		sensitive: sensitive,
	}
}

// beneath is the locator of the nodes written inside this key's value, and marks the last step as a
// sequence when the contract writes the shape as a list. `listeners` accepts only a list, while
// `operations` accepts a mapping and the list sugar stage E folds into one, so the mapping form is
// the one written here and the sugar has its own fixtures.
func (p schemaPosition) beneath() string {
	if schemaLevels[p.child].freeForm {
		return p.locator
	}
	spec, _ := schemaLevels[levelOf(p)].key(p.leaf())
	if spec.kinds&mappingValue != 0 {
		return p.locator
	}
	p.steps[len(p.steps)-1].inSequence = true
	return p.locator + "[]"
}

func (p schemaPosition) leaf() string { return p.steps[len(p.steps)-1].name }

// levelOf is the level this position's own key is declared at, which is the parent of the shape it
// names. It is re-derived rather than carried so that a position built by hand in a test cannot
// disagree with the table.
func levelOf(p schemaPosition) levelName {
	name := levelRoot
	for _, step := range p.steps[:len(p.steps)-1] {
		spec, _ := schemaLevels[name].key(step.name)
		name = spec.child
	}
	return name
}

// plainKey writes a leaf key as nothing but its name and a colon. The subjects whose dimension is
// the *position* take it so their documents do not also vary the spelling, and it is written here
// rather than taken from keySpellings[0] so that reordering that table cannot silently change which
// spelling they plant with.
var plainKey = keySpelling{name: "bare", write: func(key string) string { return key + ":" }}

// documentWriting is a document that writes value at this position and nothing else. The leaf's key
// is written in the given spelling, so a subject crossing this position dimension with the key
// dimension gets both rather than one.
func (p schemaPosition) documentWriting(value string, leaf keySpelling) string {
	var built strings.Builder

	margin := ""
	for index, step := range p.steps {
		lead := margin
		if index > 0 && p.steps[index-1].inSequence {
			lead += "- "
		}
		if index == len(p.steps)-1 {
			built.WriteString(lead + leaf.write(step.name))
			break
		}
		built.WriteString(lead + step.name + ":")
		built.WriteString("\n")
		margin = strings.Repeat(" ", runeCount(lead)+2)
	}
	built.WriteString(" " + value + "\n")
	return built.String()
}

// TestTheDeclaredPositionsCoverTheWholeTable reconciles the derivation against the table it walks,
// so a key or a level added to schema.go reaches every subject that plants at a position rather
// than silently narrowing them.
func TestTheDeclaredPositionsCoverTheWholeTable(t *testing.T) {
	positions := declaredSchemaPositions()

	seen := map[string]bool{}
	for _, at := range positions {
		if seen[at.locator] {
			t.Errorf("two derived positions share the locator %q", at.locator)
		}
		seen[at.locator] = true
	}

	// One position per declared key per path the key is reachable by, plus one probe name per
	// free-form level reached. Counted from the table rather than written down.
	want := 0
	var count func(name levelName)
	count = func(name levelName) {
		level := schemaLevels[name]
		if level.freeForm {
			want++
			return
		}
		for _, spec := range level.keys {
			want++
			if _, declared := schemaLevels[spec.child]; declared {
				count(spec.child)
			}
		}
	}
	count(levelRoot)

	if len(positions) != want {
		t.Fatalf("derived %d positions for the %d the table declares", len(positions), want)
	}
}
