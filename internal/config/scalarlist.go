package config

import (
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// This file is the one wrapper whose value is a collection: a list of scalars, each element carrying a
// position of its own.
//
// It is separate from the four scalar wrappers because it answers a question they do not. A scalar either
// converts or does not; a list can hold some elements it read and some it could not, so it accumulates
// across its own contents and anchors each refusal where the author wrote it -- which is ADR-6's element
// class rather than its value class.
//
// The never-fail guarantee is the same one scalar.go states, and it is asserted over this file by the
// same tests: the per-wrapper adversarial case, the fuzz target, and the structural assertion that no
// UnmarshalYAML in the package has an exit that can answer non-nil.

// The compile-time claim, for the reason scalar.go states it of the other four.
var (
	_ yaml.NodeUnmarshaler = (*StrList)(nil)
	_ Positioned           = StrList{}
)

// What a list declines to read. mustBeAList is the value itself being no list, which stage F refuses
// before decode; mustBeAnElement is one element of it, which stage F does not look at -- it judges that
// the key holds a list and says nothing about the list's contents, so this is the one refusal here a real
// document reaches.
var (
	mustBeAList     = fault{message: "expected a list"}
	mustBeAnElement = fault{message: "expected a list of scalars"}
)

// StrList is a list of text values: `payload.columns`, `payload.exclude`, `signing.secrets`, an
// operation's `columns`.
//
// Each element is itself a Str, so it carries its own position. That is ADR-6's element class made
// available to the rule layer: a diagnostic about a list's contents anchors on the element at fault
// (AC #33), and a list that kept only the strings would force every such rule back to the AST.
type StrList struct {
	presence
	values []Str
}

// UnmarshalYAML reads every element of this list that holds a scalar, and refuses the ones that do
// not on their own nodes.
//
// Unlike the four wrappers above, the element refusal is reachable by a real document: stage F judges
// that the key holds a list and says nothing about what the list holds, so `columns: [id, [nested]]`
// arrives here. Every element is examined before the answer is given, because the stage's policy is
// to accumulate.
func (l *StrList) UnmarshalYAML(node ast.Node) error {
	l.began(node)

	elements, isList := beneathNodeProperties(node).(*ast.SequenceNode)
	if !isList {
		l.refuse(mustBeAList)
		return nil
	}

	l.values = make([]Str, 0, len(elements.Values))
	for _, element := range elements.Values {
		var held Str
		// The element's own wrapper decides whether it holds text; what this list adds is that an
		// element it cannot read is refused in this list's words rather than in a scalar's, because
		// what the author got wrong is the list's contents.
		_ = held.UnmarshalYAML(element)
		if !held.Valid() {
			l.refuseAt(element, mustBeAnElement)
			continue
		}
		l.values = append(l.values, held)
	}
	return nil
}

// resolve extends the shared resolution with this list's elements, so each of them carries a position
// too. The elements are held unexported and are wrappers rather than decode targets, so stage G's
// walk does not reach them: a wrapper resolves what it owns (resolvewalk.go).
func (l *StrList) resolve(pass *decodePass) {
	l.presence.resolve(pass)

	for i := range l.values {
		l.values[i].resolve(pass)
	}
}
