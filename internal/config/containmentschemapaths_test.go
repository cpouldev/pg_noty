package config

import (
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// This file is how the containment oracle turns a locator schema.go declares into the paths one
// document actually writes. It is the oracle's other half (containmentschemaoracle_test.go), split
// off because the two answer different questions: that file asks what a region *is*, and this one
// asks which regions a document *holds*.

// nodeWrittenAt is the node this document writes at one concrete path, or false when it writes
// none. It is the non-fatal form of nodeIn, because expanding a sequence locator means asking for
// paths the document may not hold.
func nodeWrittenAt(root ast.Node, locator string) (ast.Node, bool) {
	selector, err := yaml.PathString(locator)
	if err != nil {
		return nil, false
	}
	node, err := selector.FilterNode(root)
	if err != nil || node == nil {
		return nil, false
	}
	return node, true
}

// sequenceLocatorMark is how declaredSchemaPositions writes a step the contract holds as a list.
const sequenceLocatorMark = "[]"

// concreteLocatorsOf expands each list step of a declared locator into the indices this document
// writes, so `$.listeners[].destination.signing.secrets` becomes one path per listener written.
//
// The count comes from the sequence node itself rather than from asking for indices until a lookup
// fails, so no document can make this loop unbounded and no lookup error has to stand in for the
// end of a list.
func concreteLocatorsOf(root ast.Node, locator string) []string {
	at := strings.Index(locator, sequenceLocatorMark)
	if at < 0 {
		return []string{locator}
	}

	sequence, exists := nodeWrittenAt(root, locator[:at])
	if !exists {
		return nil
	}
	held, isSequence := beneathNodeProperties(sequence).(*ast.SequenceNode)
	if !isSequence {
		return nil
	}

	var found []string
	rest := locator[at+len(sequenceLocatorMark):]
	for index := range held.Values {
		found = append(found,
			concreteLocatorsOf(root, locator[:at]+"["+strconv.Itoa(index)+"]"+rest)...)
	}
	return found
}
