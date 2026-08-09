package config

import (
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// This file answers where a sensitive value was written and how far it extends: which nodes one
// declaration covers, and for each, the line and column its text begins at together with the line
// the parser says it closes on. schemawalk.go decides *which* keys are sensitive; this decides what
// the redactor is then handed about each.

// locateSensitiveValues is where one declaration's sensitive value, or values, were written.
//
// The value the key holds is always one of them, whatever shape it was written in. That is the whole
// of the answer for a declaration naming a *grammar*: urlPassword reads exactly one connection
// string, so a list written there is not a list of connection strings but a shape the grammar has no
// reading for, and redactiongeometry.go takes it wholesale.
//
// A declaration naming the value as a whole splits a written list into its elements, because
// `secrets: [a, b]` is two secrets: two replacements on one line, each at coordinates the other must
// not move, and each able to point at a definition written somewhere else entirely.
func locateSensitiveValues(text *source, container ast.Node, kind sensitivity) []sensitiveValue {
	if kind != entireValue {
		located, positioned := locate(text, container, kind)
		if !positioned {
			return nil
		}
		return []sensitiveValue{located}
	}
	return locateSensitiveChildren(text, container, kind)
}

// locateSensitiveChildren preserves the inclusive extent of a sensitive flow container when
// valuesOf splits it into children, and records whether the container's interior holds bytes none
// of them claims. A reduced alias may point to a definition outside that container; only a child
// whose source line lies inside the written container may inherit its end.
func locateSensitiveChildren(
	text *source,
	container ast.Node,
	kind sensitivity,
) []sensitiveValue {
	// The same question locate asks, so it is asked through the same answer: a child inherits the
	// container's end only if it sits inside the extent the container's own text occupies.
	start := writtenStartReachingItsBlockOpener(text, container)
	end := containerEndLine(text, container)
	children := valuesOf(container)
	found := make([]sensitiveValue, 0, len(children))

	for _, child := range children {
		located, positioned := locateElement(text, child, kind)
		if !positioned {
			continue
		}
		if start.line >= firstLine && located.line >= start.line && located.line <= end &&
			end > located.throughLine {
			located.throughLine = end
		}
		found = append(found, located)
	}
	// end is already the parser's answer to "where does this container's closer sit, if it writes
	// one" -- so the interior's bounds are read from it rather than asked a second way.
	if start.line < firstLine ||
		!containerHoldsUnclaimedBytes(text, start, found, end) {
		return found
	}

	// The container's own extent joins its children, because the runes nobody claimed are outside
	// every child and blanking from a child's column leaves the ones before it: `secrets: [&name v]`
	// replaced `v` and rendered the name. It is added rather than substituted for the children,
	// because a child reduced from an alias points at a definition written elsewhere that this
	// extent does not cover (sensitivepaths.go orders the two so the container goes last).
	owner, positioned := locate(text, container, entireValue)
	if !positioned {
		return found
	}
	owner.ownsUnclaimedBytes = true
	return append(found, owner)
}

// locate records where a sensitive value a key introduced is written and what was written there.
//
// The position is where the value's *text* begins, which writtenstart.go answers by composing
// position.go's one column derivation: this file must not compute a column of its own, and a test
// enforces that by scanning for any read of the library's own (ADR-4). A node with no position was
// not read from this text -- every scalar the parser produces carries one -- so there is no line
// holding it to replace.
//
// The extent additionally reaches back to the block the value's key opened, because an author can
// write between a key's colon and the first token of the value below it and those runes belong to
// no other value.
func locate(text *source, node ast.Node, kind sensitivity) (sensitiveValue, bool) {
	return locatedAt(writtenStartReachingItsBlockOpener(text, node), node, text, kind)
}

// locateElement is locate for a value written as an element of the container holding it, whose
// extent reaches back no further than its own text.
//
// The distinction is not decoration. An element's own key is the *container's* key, so reaching
// back to it would stretch every element's extent over the elements written above it -- and
// blanking those would erase the replacements already standing there, for every list a document
// writes rather than only for one holding a gap. The lines between the container's key and its
// first element are covered once, by the container itself.
func locateElement(text *source, node ast.Node, kind sensitivity) (sensitiveValue, bool) {
	return locatedAt(writtenStartOf(text, node), node, text, kind)
}

func locatedAt(
	written writtenStart,
	node ast.Node,
	text *source,
	kind sensitivity,
) (sensitiveValue, bool) {
	if written.line < firstLine {
		// A node this text did not produce. Nothing reaches here from a document today, and the
		// refusal is asserted directly by TestLocateRefusesANodeThisTextDidNotProduce, because a
		// value dropped from the walk is a value nothing later redacts.
		return sensitiveValue{}, false
	}

	return sensitiveValue{
		line:                  written.line,
		column:                written.column,
		beginsOnALineOfItsOwn: written.beginsOnALineOfItsOwn,
		reachesBackTo:         written.reachesBackTo,
		writtenAsAContainer:   writtenAsAContainer(node),
		text:                  node.String(),
		throughLine:           containerEndLine(text, node),
		kind:                  kind,
	}, true
}

// writtenAsAContainer reports whether a node holds entries rather than text. Node properties are
// read through, because `&anchor {a: 1}` is a mapping however it is introduced -- which is the
// reduction containerEndLine and valuesOf already take at this position.
func writtenAsAContainer(node ast.Node) bool {
	switch beneathNodeProperties(node).(type) {
	case *ast.MappingNode, *ast.SequenceNode:
		return true
	default:
		return false
	}
}

// noClosingIndicator is the answer for a node whose extent this function cannot report: a scalar,
// which is no container, and a block container, which closes with nothing.
//
// It is negative rather than zero because zero used to serve as both "not a container" and "a
// container whose end the parser does not report", and no caller could tell those apart -- so a
// block container read as ending on line 0 silently disabled every reading built on the answer.
// No line is numbered below firstLine, so this cannot be mistaken for one.
const noClosingIndicator = -1

// containerEndLine is the line a mapping or a sequence closes on, when the parser reports a closing
// indicator for it.
//
// **Only a flow container has one.** A block container is delimited by indentation and writes no
// closer, so this library version leaves End nil for every block mapping and every block sequence
// -- measured, and pinned by TestGoccyReportsAClosingIndicatorForFlowContainersOnly.
//
// A block container's extent is therefore answered elsewhere, by the two consumers that need it and
// in the terms each can support: how far the value *reaches* is the indentation walk in
// sensitivereach.go, which follows a block scalar past its last child's anchor line where a derived
// last-child line would stop short; whether the container's *interior* holds unclaimed bytes is
// containerinterior.go, which derives its last line from the children because that question is
// bounded by them. Neither may be answered from a line number this function invents.
func containerEndLine(text *source, node ast.Node) int {
	switch held := beneathNodeProperties(node).(type) {
	case *ast.MappingNode:
		return closingIndicatorLine(text, held.End, node)
	case *ast.SequenceNode:
		return closingIndicatorLine(text, held.End, node)
	default:
		return noClosingIndicator
	}
}

func closingIndicatorLine(text *source, closer *token.Token, node ast.Node) int {
	if closer == nil {
		return noClosingIndicator
	}
	return tokenPosition(text, closer, pathOf(node)).Line()
}
