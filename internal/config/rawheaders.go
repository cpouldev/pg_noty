package config

import (
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// This file decodes a header mapping, whose key names the contract does not declare: an author may
// send any HTTP field they like, so the shape check has nothing to say about the names here and every
// rule about them is a rule about the name itself (R19, R20, R21).
//
// **Each key keeps its own position, which is why this is not a Go map.** Three rules anchor on a
// header *name*: an invalid field name, a name reserved by pg_noty, and two names differing only in
// case -- and the third has to name the line the first occurrence was written on. A
// map[string]string could carry none of that, and its iteration order would additionally decide which
// of two spellings survived a merge.
//
// Ordering is the mapping's own, which is the order the entries arrive in rather than document order:
// after stage E an inherited entry sits last while carrying an earlier line (keyrepeats.go's
// entriesInWrittenOrder is what a *first occurrence* rule needs, and that rule is stage F's). Nothing
// here depends on the order, and every diagnostic about a header is sorted by position at the
// boundary, so the arrival order is deterministic without being document order.

// The compile-time claim, for the same reason rawOperations states it.
var (
	_ yaml.NodeUnmarshaler = (*rawHeaders)(nil)
	_ Positioned           = rawHeaders{}
)

// What this decode declines to read. Reachable by no document -- stage F declares `headers` a mapping
// and refuses a key it cannot read -- and reached directly by
// TestTheHeadersDecodeRefusesAShapeItCannotReadRatherThanFailing.
// The mapping refusal names its subject, for the reason rawoperations.go's does.
var (
	mustBeAHeaderMapping = fault{message: "expected a mapping of header names"}
	mustBeAHeaderName    = fault{message: "expected a header name"}
)

// rawHeaders is one header mapping as written.
type rawHeaders struct {
	presence
	// values are held unexported for the reason rawOperations' are: they are wrappers, and this type
	// resolves them itself.
	values []writtenHeader
}

// writtenHeader is one header: the field name as the author wrote it, and the value beside it. Both
// are positioned, because a rule about a header name anchors on the name and a rule about its value
// anchors on the value.
type writtenHeader struct {
	Name  Str
	Value Str
}

// UnmarshalYAML reads every entry of a header mapping.
//
// A value that is not a scalar is the one refusal here a document can reach: a header level declares
// no keys, so stage F asks nothing of what sits beneath one and `X-Trace: {a: 1}` arrives intact. Its
// own wrapper refuses it, on its own node.
func (h *rawHeaders) UnmarshalYAML(node ast.Node) error {
	h.began(node)

	entries, isMapping := beneathNodeProperties(node).(*ast.MappingNode)
	if !isMapping {
		h.refuse(mustBeAHeaderMapping)
		return nil
	}

	h.values = make([]writtenHeader, 0, len(entries.Values))
	for _, entry := range entries.Values {
		name, readable := keyTextOf(entry.Key)
		if !readable {
			h.refuseAt(entry.Key, mustBeAHeaderName)
			continue
		}

		written := writtenHeader{}
		written.Name.began(entry.Key)
		written.Name.value = name
		// An empty name is not refused here: R19 owns "a header name is a valid HTTP field name", and
		// refusing it twice would be two diagnostics for one mistake.
		_ = written.Value.UnmarshalYAML(entry.Value)

		h.values = append(h.values, written)
	}
	return nil
}

// resolve extends the shared resolution with each header's name and value.
func (h *rawHeaders) resolve(pass *decodePass) {
	h.presence.resolve(pass)

	for i := range h.values {
		h.values[i].Name.resolve(pass)
		h.values[i].Value.resolve(pass)
	}
}
