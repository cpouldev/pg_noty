package config

import "github.com/goccy/go-yaml/ast"

// This file answers what a position in the document *is*, so that interpolate.go can say
// what happens at one. It holds the whole of V2's rule: a value is what is reached through
// a value edge, a key is what is reached through a key edge, and neither is decided by the
// node's own type -- because mapping keys, anchor names, alias names and scalar values are
// all *ast.StringNode.
//
// Nothing here knows about the environment, the grammar or diagnostics, which is what lets
// the value-position rule be tested on its own rather than through a substitution.

// scalarText is a scalar's text together with the node a diagnostic about that text points
// at.
//
// The two are not always the same node. A block scalar keeps its content in an inner string
// node whose token the parser reports on the block's *last* line, while the block's own node
// is anchored on its `|` or `>` indicator -- which is where a diagnostic belongs. Measured:
// for `when: |\n  OLD ${S}\n`, the block reports [1:7] and its inner node [3:7], a position
// on a line holding no part of the value (Step 1's Implementation Note 8, re-measured as
// Step 3's Note 1).
type scalarText struct {
	anchor ast.Node
	holder *ast.StringNode
}

// written is the text as the configuration file holds it, decoded: a quoted scalar's quotes
// are the parser's business and not this stage's.
func (s scalarText) written() string { return s.holder.Value }

// rewrite replaces that text. It writes to the node and to nothing else, which is what keeps
// the source bytes -- and therefore every rendered snippet -- as they were read.
func (s scalarText) rewrite(text string) { s.holder.Value = text }

// scalarTextOf reports the text a node holds, when it holds text at all.
func scalarTextOf(node ast.Node) (scalarText, bool) {
	switch held := node.(type) {
	case *ast.StringNode:
		return scalarText{anchor: held, holder: held}, true
	case *ast.LiteralNode:
		return scalarText{anchor: held, holder: held.Value}, true
	}
	return scalarText{}, false
}

// keyTextOf is the text a mapping key is written as, decoded, together with whether this
// stage recognises the key's shape at all.
//
// Every spelling YAML permits for one key -- bare, single-quoted, double-quoted, introduced by `?`,
// anchored, tagged -- is read the same way, and every shape that carries no text of its own is answered by
// name -- cleared here or by nontextualscalar.go's enumeration, or refused here -- rather than asked for its
// rendering. Asking would gate the answer on evidence the input supplies, which is the shape of guard the
// value path's default arm exists to avoid. The enumeration is what makes refusing everything unlisted
// affordable, exactly as it is there: TestEveryShapeADocumentCanHoldInAKeyPositionIsRecognised asserts that
// every shape but one is cleared, and the exception -- an alias -- is refused below and by its own named
// test.
//
// The wrappers a key can be written inside are read off by the package's one answer to that
// question rather than by arms of this switch (nodeproperties.go). Each carries the key one
// level down while its own token is only the introducer -- `?`, `&name` or `!!tag` -- so reading
// that token would miss every reference written this way. Measured: for `? key`, the node's
// GetToken().Value is "?" and the key text sits in .Value (Step 3's Implementation Note 2).
func keyTextOf(key ast.Node) (text string, recognised bool) {
	beneath := beneathKeyProperties(key)

	if scalar, holdsText := scalarTextOf(beneath); holdsText {
		return scalar.written(), true
	}
	if isNonTextualScalar(beneath) {
		return "", true
	}

	switch beneath.(type) {
	case *ast.AliasNode:
		// An alias in a key position is the one shape this stage cannot clear. It holds a
		// name today, but stage E replaces it with its anchor's value -- and that value sits
		// in a value position, so stage D has already substituted the environment into it.
		// `a: &k ${SECRET}` followed by `*k: v` would therefore become a key made of the
		// environment's bytes, which is the one outcome AC #6 exists to prevent, reached
		// after the check that would have caught it has run. This stage holds no anchor
		// table, so it cannot answer what the key will say and must not pass it.
		return "", false
	case *ast.MergeKeyNode:
		// `<<` is the merge indicator itself: it names no variable, and what it merges is
		// stage E's question rather than this stage's.
		return "", true
	}
	return "", false
}

// keyPositionsOf reports the key nodes written directly beneath node. Only a mapping has
// any, so every other shape yields none.
//
// `*ast.MappingValueNode` -- the entry a mapping is made of -- is deliberately absent, for
// the reason `*ast.SequenceEntryNode` is absent from valuePositionsOf: an entry is reached
// only through its mapping, which already yields the entry's key, so answering for it here
// would be a second path to the same key. Measured: this library version wraps every
// key/value pair through newMappingNode, so a nested mapping arrives as an *ast.MappingNode
// and no entry is ever handed to either function (Step 3's Implementation Note 4).
func keyPositionsOf(node ast.Node) []ast.Node {
	mapping, isMapping := node.(*ast.MappingNode)
	if !isMapping {
		return nil
	}
	return oneNodePerEntry(mapping, func(entry *ast.MappingValueNode) ast.Node { return entry.Key })
}

// valuePositionsOf reports the nodes directly beneath node that sit in a value position, and
// whether this stage recognises node's shape at all.
//
// This is V2's requirement made into a function: a value is what is reached through a value
// edge, never what carries a particular node type. The three shapes a naive type switch
// corrupts are all excluded here by construction -- a mapping yields its entries' values and
// not their keys, an anchor yields its value and not its name, and an alias yields nothing
// at all.
//
// The default arm is unsafe on purpose. Every shape that holds no value is listed, so a shape
// nobody listed is over-handled by the caller instead of being walked past.
//
// Three groups of shape are deliberately absent. The two scalarTextOf answers for are omitted
// so that a caller must ask scalarTextOf first: one that asked in the other order would fail
// loudly on every string in the document rather than quietly stop substituting. The two
// entry wrappers -- `*ast.SequenceEntryNode` and `*ast.MappingValueNode` -- are omitted
// because this library version reaches an element only through its container, which already
// yields the element itself; answering for a wrapper would visit every element twice and
// unescape `$${` twice (Step 3's Implementation Note 4, the same measurement keyPositionsOf
// records).
func valuePositionsOf(node ast.Node) (values []ast.Node, recognised bool) {
	if isNonTextualScalar(node) {
		return nil, true
	}

	switch held := node.(type) {
	case *ast.MappingNode:
		return oneNodePerEntry(held, func(entry *ast.MappingValueNode) ast.Node { return entry.Value }), true
	case *ast.SequenceNode:
		// Values, not the Elements skill Pattern 9's own example iterates: this version of the
		// library has no such field, only Values and Entries (Step 3's Implementation Note 3),
		// and Values holds the very nodes Entries does, which
		// TestGoccyGivesASequenceTheSameNodesInValuesAndEntries pins.
		return held.Values, true
	case *ast.AnchorNode:
		// Never held.Name. An anchor's name is the identifier every alias to it resolves
		// through, and rewriting it would break the document elsewhere (V2).
		return []ast.Node{held.Value}, true
	case *ast.TagNode:
		return []ast.Node{held.Value}, true
	case *ast.AliasNode:
		// An alias site holds a name, not content: the content was substituted where the
		// anchor was defined. That is what makes `$${` unescape exactly once however many
		// aliases reach it, and it is why stage D runs before merge keys are expanded.
		return nil, true
	}
	return nil, false
}

// oneNodePerEntry is the node side selects from every entry a mapping holds: its keys, or its
// values.
//
// The two are one walk over the same entries differing only in the field read, so they are one
// function rather than two that could drift into visiting different sets.
func oneNodePerEntry(mapping *ast.MappingNode, side func(*ast.MappingValueNode) ast.Node) []ast.Node {
	nodes := make([]ast.Node, 0, len(mapping.Values))
	for _, entry := range mapping.Values {
		nodes = append(nodes, side(entry))
	}
	return nodes
}
