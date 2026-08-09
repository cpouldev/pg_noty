package config

import "github.com/goccy/go-yaml/ast"

// This file is YAML's node properties -- the `&anchor` and the `!!tag` any node may carry, and
// the `?` only a key may -- and how a position that wants a particular shape reads past them.
//
// One enumeration, not one per asker. A position that listed fewer wrappers than the grammar
// permits in it refuses a legal document, and refuses it through the arm meant for a different
// mistake; a position that
// enumerated them itself would be where the set drifts. Three questions read past these wrappers
// -- what text a key holds, what makes two keys the same key, and what a `<<` points at -- and
// they all read past them here.
//
// Recording an anchor is the one thing that cannot be asked of this file: a peel discards the
// wrapper the anchor is, so anchor.go's rememberAnchoredKey walks the same three arms instead,
// and TestAnAnchorWrittenOnAKeyIsAvailableToLaterAliases is what keeps the two sets one set.

// beneathNodeProperties is the value a node carries once the properties YAML permits to be
// written on it have been read off.
//
// An anchor and a tag are properties *of* the node beneath them rather than nodes of their own,
// so a position wanting a particular shape has to look past both before it asserts one. It loops
// rather than unwrapping once because `&m !!map {a: 1}` writes both, in either order.
func beneathNodeProperties(node ast.Node) ast.Node {
	for {
		switch held := node.(type) {
		case *ast.AnchorNode:
			node = held.Value
		case *ast.TagNode:
			node = held.Value
		default:
			return node
		}
	}
}

// beneathKeyProperties is the same answer for a key: what beneathNodeProperties reads off a
// value, plus the `?` only a key can carry. It is that function with one wrapper more rather than
// a second enumeration of the same two.
func beneathKeyProperties(key ast.Node) ast.Node {
	for {
		beneath := beneathNodeProperties(key)

		explicit, isExplicit := beneath.(*ast.MappingKeyNode)
		if !isExplicit {
			return beneath
		}
		key = explicit.Value
	}
}
