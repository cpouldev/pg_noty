package config

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// This file is ADR-6's second anchor class: where a diagnostic about a whole mapping points, which
// is the first key the mapping's author wrote in it.
//
// It is a file of its own rather than a paragraph of position.go because the mapping's node no
// longer answers the question by itself. Stage E rewrites a mapping's entries -- expandedWith
// appends the entries a `<<` brings in after the ones written here, each keeping the anchor's own
// line and its own locator (mergekey.go) -- so "the first key" is neither the first entry of the
// slice nor the earliest entry by position. It is the earliest of the entries this mapping's own
// author wrote, and a mapping can have none of those at all.

// The two ways one YAML locator extends another: a named key beneath it, and an element of it.
// Both are needed, because the operations fold gives a folded entry the element locator its list
// form had (listsugar.go) while a hand-written mapping gives its entries named ones.
const (
	pathSeparator = "."
	pathElement   = "["
)

// positionOfMapping anchors a diagnostic about a whole mapping on the first key its author wrote in
// it. A mapping's own token is the colon of its first entry -- measured, and pinned by
// TestGoccyGivesAMappingTheColonOfItsFirstEntry -- so anchoring on it would put every mapping-level
// caret on punctuation. The locator stays the mapping's own, because the diagnostic is about the
// mapping rather than about that key.
//
// Two shapes have no such key and fall back to the mapping's own token. A node that is not a
// mapping has no keys at all. A mapping every entry of which was written somewhere else -- a
// listener spelled `- <<: *shared` and nothing more -- has keys, but none of them is in this
// mapping, and for it the fallback is the better of two imperfect answers rather than a lapse:
// punctuation inside the mapping the author has to fix, against a key inside the mapping they
// merged. TestAListenerInheritingEveryKeyAnchorsItsMissingKeysOnItself is that choice.
func positionOfMapping(src *source, node ast.Node) Positioned {
	mapping, isMapping := node.(*ast.MappingNode)
	if !isMapping {
		return positionOf(src, node)
	}

	first, wroteOne := firstKeyWrittenIn(src, mapping)
	if !wroteOne {
		return positionOf(src, node)
	}
	return tokenPosition(src, first.GetToken(), pathOf(node))
}

// firstKeyWrittenIn is the earliest key this mapping's author wrote in it, and whether they wrote
// one at all. Presence is its own result, because "no key of its own" is an answer the caller has
// to act on rather than an empty value it can report.
//
// Neither half of the answer is mapping.Values[0]. Which entries belong here is decided by their
// locators, because an inherited one still names the mapping it was written in; and which of those
// is first is decided by their positions, because a slice a later pass rewrites carries no order
// worth reading.
func firstKeyWrittenIn(src *source, mapping *ast.MappingNode) (ast.MapKeyNode, bool) {
	var first ast.MapKeyNode
	var at Positioned
	found := false

	for _, entry := range mapping.Values {
		if !writtenIn(mapping, entry.Key) {
			continue
		}

		where := positionOf(src, entry.Key)
		if !found || comparePositions(where, at) < 0 {
			first, at, found = entry.Key, where, true
		}
	}
	return first, found
}

// writtenIn reports whether the document wrote this key inside this mapping.
//
// A node's locator names where the document holds it, and stage E appends an inherited entry
// without re-pathing it -- so a key brought in by `<<` still names the anchor's mapping while a key
// written here names this one. Sharing a prefix is not enough: the locator has to be *extended*,
// or `$.list` would claim every key of `$.listeners`.
//
// A mapping carrying no locator of its own claims no entry, which is the fail-closed direction: it
// anchors on its own token rather than on a key that may belong to another mapping.
func writtenIn(mapping ast.Node, key ast.Node) bool {
	held, locator := mapping.GetPath(), key.GetPath()
	if !strings.HasPrefix(locator, held) {
		return false
	}

	within := locator[len(held):]
	return strings.HasPrefix(within, pathSeparator) || strings.HasPrefix(within, pathElement)
}
