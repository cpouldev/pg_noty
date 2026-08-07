package config

import (
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// This file is the other half of "decode cannot fail": the two shapes the decoder has to *fill*
// rather than convert -- a mapping with a declared shape beneath it, and a list of them.
//
// **Why they are wrappers rather than plain struct fields, measured.** A wrapper makes every scalar
// safe, and leaves the containers as the one way a decode can still fail. Two ways, measured on
// v1.19.2:
//
//   - `database: !!map {url: x}` fails yaml.NodeToValue with `tag was used where mapping is expected`.
//     That document is legal YAML, stage E deliberately leaves the tag standing, and stage F
//     deliberately accepts it -- valid/shape_ok_values_carrying_node_properties.yaml is a committed
//     fixture that writes `!!map`, `!!seq` and a tagged list element. So a file the whole pipeline
//     calls clean would have failed the decoder, and stage F's stop could not have protected it
//     because stage F has nothing to complain about.
//   - `database: text` and a list element that is not a mapping fail it too. Stage F does stop both,
//     so those are refusals no document reaches -- but relying on that would make "decode cannot
//     fail" a property of another stage rather than of this one.
//
// Reading past the node properties here is the same reduction every other position in this package
// performs, and recording the shape refusal instead of returning it is what makes the guarantee
// structural: after these two types there is no field in the raw tree whose decode can answer
// non-nil. That source-level property is enforced by TestNoWrapperHasAnExitThatCanReturnAnError; the
// property-spelling corpus separately proves these wrappers peel every legal container spelling and
// still fill the value beneath it.
//
// The generic parameter is what keeps this one implementation rather than one per level: nine mapping
// levels share these ten lines instead of nine near-identical unmarshalers.

// The compile-time claim, for the two instantiations the raw tree uses. A container that stopped
// implementing the interface would decode as a plain struct -- the value beneath it silently empty --
// which no test would name.
var (
	_ yaml.NodeUnmarshaler = (*mappingOf[rawDatabase])(nil)
	_ yaml.NodeUnmarshaler = (*listOf[rawListener])(nil)
	_ Positioned           = mappingOf[rawDatabase]{}
	_ Positioned           = listOf[rawListener]{}
)

// What a container declines to read. None of the three is reachable by a document -- stage F refuses a
// value whose YAML shape is not the one the contract declares for its key, and stops the run -- so all
// three are reached directly by TestAContainerRefusesAShapeItCannotFillRatherThanFailingTheDecode.
//
// The third is scalarlist.go's mustBeAList rather than a variable of this file's. "The value here is not a
// list" is one condition with one wording, and two declarations holding one text are two things to keep in
// step and one more way to raise the wrong one with nothing failing
// (TestNoTwoRefusalsShareOneWording). What does differ between the two files is the *element* refusal, so
// mustBeAnEntryOf and mustBeAnElement stay separate: a list of mappings and a list of scalars are
// unreadable for different reasons and an author needs to be told which.
var (
	mustBeAMapping  = fault{message: "expected a mapping"}
	mustBeAnEntryOf = fault{message: "expected a list of mappings"}
)

// mappingOf is one mapping of the configuration whose keys the contract declares, decoded into T.
//
// Value is exported because stage G's walk descends through it to reach the wrappers beneath: it is a
// plain decode target rather than a wrapper, so it cannot resolve itself.
type mappingOf[T any] struct {
	presence
	Value T
}

func (m *mappingOf[T]) UnmarshalYAML(node ast.Node) error {
	m.began(node)

	if err := yaml.NodeToValue(beneathNodeProperties(node), &m.Value); err != nil {
		m.refuse(mustBeAMapping)
	}
	return nil
}

// listOf is a list of mappings of one declared shape -- which the contract has exactly one of,
// `listeners`.
//
// Values is exported for the same reason mappingOf's is. Every element is decoded even when an
// earlier one could not be, because the stage's policy is to accumulate; an element it could not read
// is left out rather than left half-filled.
type listOf[T any] struct {
	presence
	Values []T
}

func (l *listOf[T]) UnmarshalYAML(node ast.Node) error {
	l.began(node)

	elements, isList := beneathNodeProperties(node).(*ast.SequenceNode)
	if !isList {
		l.refuse(mustBeAList)
		return nil
	}

	l.Values = make([]T, 0, len(elements.Values))
	for _, element := range elements.Values {
		var held T
		if err := yaml.NodeToValue(beneathNodeProperties(element), &held); err != nil {
			l.refuseAt(element, mustBeAnEntryOf)
			continue
		}
		l.Values = append(l.Values, held)
	}
	return nil
}
