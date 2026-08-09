package config

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// This file translates between the two vocabularies for the shape of a value: the library's node
// types, and the three kinds schema.go declares a key's value in. It is where a wrong-node-kind
// diagnostic gets both its verdict and its wording, so the two cannot describe different sets.

// kindNames is what each declared kind is called in a diagnostic, in the order a message lists
// them. It is a table rather than a switch because a message about `operations` has to name two
// kinds, and because a kind added to the vocabulary with no name here would render as nothing --
// which TestEveryDeclaredKindHasAName is what prevents.
var kindNames = []struct {
	kind nodeKinds
	name string
}{
	{scalarValue, "a scalar"},
	{mappingValue, "a mapping"},
	{sequenceValue, "a list"},
}

// nodeKindOf is the kind a node has in the contract's vocabulary, and whether the contract has a
// word for its shape at all.
//
// The two shapes that carry text are asked about through scalarTextOf and the rest through
// nontextualscalar.go's enumeration, so this reader recognises exactly the scalars the other
// positions recognise rather than listing them a third time. The node is expected to have had its
// properties read off already: an anchor and a tag are properties of the value beneath them and
// not shapes of their own.
//
// The unrecognised answer is the one that matters. A shape nobody listed is refused rather than
// waved through, because waving it through would hand the decoder a value the contract never
// judged.
func nodeKindOf(node ast.Node) (nodeKinds, bool) {
	if _, holdsText := scalarTextOf(node); holdsText {
		return scalarValue, true
	}
	if isNonTextualScalar(node) {
		return scalarValue, true
	}

	switch node.(type) {
	case *ast.MappingNode:
		return mappingValue, true
	case *ast.SequenceNode:
		return sequenceValue, true
	}
	return 0, false
}

// namesOfKinds is how a diagnostic says which shapes a key accepts: "a mapping", or "a mapping or
// a list" for the one key that takes either.
func namesOfKinds(kinds nodeKinds) string {
	var named []string
	for _, kind := range kindNames {
		if kinds&kind.kind != 0 {
			named = append(named, kind.name)
		}
	}
	return strings.Join(named, " or ")
}

// holdsNothing reports whether a container holds no entries and no elements.
//
// A scalar is not a container, so it is never empty in this sense: where the contract requires a
// container, the kind check has already refused a scalar, and asking this of one afterwards would
// add a second diagnostic about a value that has no entries because it can have none.
func holdsNothing(node ast.Node) bool {
	switch held := node.(type) {
	case *ast.MappingNode:
		return len(held.Values) == 0
	case *ast.SequenceNode:
		return len(held.Values) == 0
	}
	return false
}
