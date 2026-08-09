package config

import (
	"strconv"

	"github.com/goccy/go-yaml/ast"
)

// This file is what the contract requires of the value beneath a declared key: that its YAML shape
// is one the key accepts, that it holds something when the contract says it must, and that
// whatever shape it declares beneath it is judged in turn.
//
// What those shapes are *called*, and which of them a node has, is nodekind.go's.

// mustHold is what a value of the wrong shape is told: the shapes its key accepts, and -- for the
// one key whose two accepted forms are worth spelling out rather than naming -- how to write each
// of them (AC #16).
func mustHold(spec keySpec) fault {
	return fault{
		message: strconv.Quote(spec.name) + " must be " + namesOfKinds(spec.kinds),
		hint:    spec.shapeHint,
	}
}

// mustHoldSomething is what an empty container is told.
func mustHoldSomething(name string) fault {
	return fault{message: strconv.Quote(name) + " must hold at least one entry"}
}

// unreadableValueShape is what a value whose shape the contract has no word for is refused with,
// in the words stage D already uses for the same condition.
//
// No document reaches it either: nodeKindOf names every shape this library version can put in a
// value position, and stage E removes the two it does not name from that position before this
// stage runs. TestAValueShapeThisStageCannotNameIsRefusedRatherThanWalkedPast reaches it by
// construction instead.
var unreadableValueShape = fault{
	message: unrecognisedShapeMessage,
	hint:    unrecognisedShapeHint,
}

// checkValue judges what one declared key holds: its YAML shape, whether it holds anything at all
// when the contract requires it to, and whatever the shape beneath it declares.
func (p *shapePass) checkValue(spec keySpec, value ast.Node) {
	// The properties an author may write on a value are read off before its shape is asserted:
	// `database: !!map {url: x}` is legal YAML, and stage E leaves the tag standing because a tag
	// is a claim about the value rather than a value of its own.
	held := beneathNodeProperties(value)

	kind, recognised := nodeKindOf(held)
	if !recognised {
		p.report(held, unreadableValueShape)
		p.undecodable = true
		return
	}
	if kind&spec.kinds == 0 {
		p.report(held, mustHold(spec))
		p.undecodable = true
		return
	}

	if spec.emptiness != noRule && holdsNothing(held) {
		p.reportAs(spec.emptiness, held, mustHoldSomething(spec.name))
		return
	}

	child, declared := schemaLevels[spec.child]
	if !declared {
		return
	}
	// A key names one shape however many of that shape it holds: `listeners` holds a list of
	// listener mappings, and `database` holds one database mapping. valuesOf is the package's
	// answer to that question, shared with the redaction walk (schemawalk.go).
	for _, node := range valuesOf(held) {
		p.checkMapping(child, node, spec.name)
	}
}
