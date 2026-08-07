package config

import "github.com/goccy/go-yaml/ast"

// This file is what makes two keys the same key. Two of stage E's answers depend on it -- the
// expansion asks whether the author already wrote a key a source brings in, and the operations
// fold asks whether an element names an operation another element already named -- so it is one
// question with one home rather than the merge expansion's private notion of sameness.
//
// The wrappers a key can be written inside are nodeproperties.go's.

// textualKey prefixes the identity of a key that carries text, so that it cannot collide with
// the identity of a key that carries none. No node type renders as this word.
const textualKey = "text "

// keyIdentity is what makes two keys the same key.
//
// Three things could tell two keys apart, and only one of them may. The *wrapper* may not: `url`,
// `'url'`, `? url`, `&k url` and `!!str url` are one key rather than five (Step 3's
// Implementation Note 16), and so are `16`, `? 16`, `&k 16` and `!!int 16`, which is why both
// halves are decided about the node beneath the key's properties. The *rendering* may not
// either: `16` and `0x10` are the integer sixteen written twice, and `true`, `True` and `TRUE`
// are one boolean. Only the *value* may -- the kind the parser read, and what it read.
//
// Text is read through scalarTextOf, this package's answer to "does this node hold text", so a
// key that holds none is identified by its kind together with the value the parser read from it.
// Reading it through a helper that answers empty text for those is what would make every one of
// them the same key as `"": v`, and a merge would then hold the first of them and silently drop
// the rest.
func keyIdentity(key ast.MapKeyNode) string {
	beneath := beneathKeyProperties(key)

	if text, holdsText := scalarTextOf(beneath); holdsText {
		return textualKey + text.written()
	}
	return beneath.Type().String() + " " + nonTextualKeyValue(beneath)
}

// nonTextualKeyValue is what tells two keys of one kind apart: the value the parser read, for the
// kinds that hold one, and the node's rendering for the kinds that hold none.
//
// Which kinds hold one is nontextualscalar.go's enumeration rather than a switch here, because a
// position asks the same question of the same set and two switches over one set drift.
//
// The fallback is the rendering, and it splits one key into two rather than merging two into one
// -- which is the *unsafe* direction, not the safe one an earlier draft of this comment claimed.
// Two spellings of one key that identify apart both survive a merge, so the mapping handed to the
// decoder holds that key twice and the merged entry wins over the one the author wrote directly:
// V5's precedence inversion, reached through this package's own code. The fallback is affordable
// only because nothing the parser reads as a value can reach it. Two shapes do, and neither has
// more than one spelling: the merge marker `<<`, and an alias, which is refused as a key long
// before identity is asked (TestAnAliasKeyIsRefusedBecauseStageEResolvesItToASubstitutedValue).
func nonTextualKeyValue(node ast.Node) string {
	if value, readAsAValue := parsedScalarValue(node); readAsAValue {
		return value
	}
	return node.String()
}

// keyIdentitiesOf is the set of keys a run of entries holds.
func keyIdentitiesOf(entries []*ast.MappingValueNode) map[string]bool {
	held := make(map[string]bool, len(entries))
	for _, entry := range entries {
		held[keyIdentity(entry.Key)] = true
	}
	return held
}
