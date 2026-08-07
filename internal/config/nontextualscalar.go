package config

import (
	"fmt"

	"github.com/goccy/go-yaml/ast"
)

// This file is the one enumeration of the scalars the parser has already read as something other
// than text -- a number, a boolean, an infinity, a not-a-number, or nothing at all -- together
// with the value it read from each.
//
// Two questions are asked of that set, and they have to be asked of the same set. A position
// asks whether a node can hold `${`, and the answer is no for every kind here: text carrying
// those two characters would have been read as a string. Key identity asks what tells two keys of
// one kind apart, and the answer is the value the parser read, because `16` and `0x10` are the
// integer sixteen written twice (Step 4's Implementation Note 18).
//
// They were two switches over one set until Step 5, with nothing tying them, and the drift they
// admitted was not symmetrical. A kind the first accepted and the second lacked fell through to
// the node's *rendering*, which splits one key into two -- and a merge then admits a second
// spelling of a key the target already holds, so the decode holds that key twice and the merged
// entry wins. That is V5's silent precedence inversion, reached through this package's own code,
// with nothing failing. One enumeration, read two ways.

// parsedScalarValue is the value the parser read from a node that carries no text, and whether
// this is such a node at all. Presence is its own result, so the empty value null and
// not-a-number carry cannot read as "this kind is unknown".
//
// The value is rendered here rather than handed back as the library holds it, because its one
// consumer compares it with another of the same kind. Collapsing the several ways YAML lets one
// number or one boolean be written is the point, and it is safe precisely because that consumer
// carries the kind alongside: `16` and `16.0` render alike here and stay apart as an Integer and
// a Float, which is how YAML itself keeps them apart.
func parsedScalarValue(node ast.Node) (string, bool) {
	switch held := node.(type) {
	case *ast.IntegerNode:
		// Value is a uint64 for a positive literal and an int64 for a negative one, measured on
		// v1.19.2; %v renders both as the number, which is what makes `16` and `0x10` one key
		// while keeping `16` and `-16` two.
		return fmt.Sprintf("%v", held.Value), true
	case *ast.FloatNode:
		return fmt.Sprintf("%v", held.Value), true
	case *ast.BoolNode:
		return fmt.Sprintf("%v", held.Value), true
	case *ast.InfinityNode:
		// The sign is the value here: `.inf` and `-.inf` are two keys.
		return fmt.Sprintf("%v", held.Value), true
	case *ast.NullNode, *ast.NanNode:
		// One value each, so the kind alone identifies it and there is nothing to add. Every
		// spelling of null is one value, and so is every spelling of not-a-number.
		return "", true
	}
	return "", false
}

// isNonTextualScalar reports whether the parser has already read this node as something other
// than text -- a number, a boolean, or nothing at all.
//
// None of them can hold `${`: text carrying those two characters would have been read as a
// string. It is the presence half of the answer above rather than a switch of its own, so a kind
// the library adds reaches both askers or neither.
func isNonTextualScalar(node ast.Node) bool {
	_, readAsAValue := parsedScalarValue(node)
	return readAsAValue
}
