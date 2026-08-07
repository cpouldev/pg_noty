package config

import "github.com/goccy/go-yaml/ast"

// This file finds every value the schema table declares sensitive, by walking the document alongside
// the table that describes it. Where those values are is the question; what happens to them is
// `sensitivepaths.go`'s answer, and the split is what keeps a walk from carrying knowledge of
// redaction.
//
// The walk is hand-rolled rather than driven by the library's generic one, per skill Pattern 3 and
// because a per-document walk is the only correct form here (V1).

// sensitiveValue is one value the schema declares sensitive: where it was written, what was
// written there, and how much of it must go.
type sensitiveValue struct {
	line int
	// column is the rune column the value's own text begins at, derived by writtenstart.go from
	// position.go's one derivation rather than computed here (ADR-4).
	column columnInRunes
	// beginsOnALineOfItsOwn is writtenstart.go's other answer, carried because the reach walk asks
	// the same question of the same shape and gets it wrong on its own (sensitivereach.go).
	beginsOnALineOfItsOwn bool
	// reachesBackTo is the first line of the value's redactable extent, which is earlier than line
	// only for a value written below its key with lines in between. It is a field of its own
	// because line and column answer the *replacement* question and this one answers the *extent*
	// question; conflating them would move every caret that reads the first.
	//
	// The field is declared in this file because the struct it belongs to is, and for no other
	// reason: writtenstart.go owns the answer and every other file only carries or consumes it.
	// That is the whole of the opener-gap change's reach outside writtenstart.go's own file set,
	// and it is stated here rather than left to a reader's diff because a package worked on by
	// more than one author has no other place to notice it. Which file may name this field, and
	// what question each one answers with it, is machine-checked by
	// TestEveryReaderOfTheRedactableExtentIsClassified.
	reachesBackTo int
	// writtenAsAContainer is whether the value's own node is a mapping or a sequence rather than a
	// scalar, which is what tells a declaration naming a *grammar* that its grammar does not apply
	// (redactiongeometry.go).
	writtenAsAContainer bool
	text                string
	// throughLine is the line the container this value belongs to closes on, when the parser
	// reports a closing indicator for it, and noClosingIndicator otherwise.
	throughLine int
	// ownsUnclaimedBytes is whether this value is a container whose interior holds author-written
	// runes no child of it claims -- an anchor name, a tag, a comment between two elements. A value
	// declared secret in full owns them, and only blanking its own extent wholesale takes them
	// (containerinterior.go).
	ownsUnclaimedBytes bool
	kind               sensitivity
}

// sensitiveValues walks the document alongside the schema level that describes it and reports
// every value the table declares sensitive.
//
// A declared path is exact: destination.url stays public while database.url loses only its
// password. Beneath an undeclared key no such distinction exists, so matching the schema-derived
// sensitive leaf names is deliberately conservative. That over-redaction is the safe answer to an
// unknown context; it never affects destination.url under its declared destination parent.
func sensitiveValues(text *source, level mappingLevel, node ast.Node) []sensitiveValue {
	// A mapping with one entry is still a MappingNode in this library version, measured and
	// pinned by TestGoccyGivesEveryMappingAMappingNode. Node properties do not change that
	// shape: a mapping used as a child or sequence element may carry either property.
	mapping, isMapping := beneathNodeProperties(node).(*ast.MappingNode)
	if !isMapping {
		// The table says a mapping of this level is written here and something else was. Which names
		// are legal is a statement about a mapping, so it says nothing about a sequence or a scalar
		// written in its place: the shape is undescribed and what is inside it fails closed.
		return sensitiveValuesBeneathAnUndeclaredShape(text, node)
	}

	var found []sensitiveValue
	for _, entry := range mapping.Values {
		// One answer to "what text is this key", shared with stage D (valueposition.go), so a
		// key written `? url` or `!!str url` is the declared key it names rather than the
		// introducer its own token holds (Step 3's Implementation Note 2: a wrapped key's own
		// token is only its introducer).
		name, recognised := keyTextOf(entry.Key)
		if !recognised {
			// Fail closed, which on this path means over-redact. Sensitivity is looked up by
			// key text, so a key with no readable text has no declaration to consult -- and
			// "nothing is declared here" is the one answer redaction may not default to,
			// because it renders whatever is beneath in the clear. An alias key is the shape
			// that reaches this: it names an anchor this walk does not resolve, so the value
			// under it could be `database.url` and could be anything else. Taking the value
			// wholesale is the direction redactValue already takes for a value whose extent
			// cannot be read off its line (D3).
			if hidden, positioned := locate(text, entry.Value, entireValue); positioned {
				found = append(found, hidden)
			}
			continue
		}

		spec, declared := level.key(name)
		if !declared {
			// A free-form level declares every *name* public. Its lack of fixed key specs is not an
			// unknown context and must not borrow sensitivity from the same leaf name at another
			// path, which is why the name is not consulted here. It declares no *shape* though: a
			// header's value is a scalar, and a container written where one belongs is a context
			// the table does not describe, so what is inside it is asked as an unknown. Both sides
			// are named: TestAFreeFormHeaderNamedLikeASensitiveKeyRemainsPublic keeps the
			// exemption, and TestEveryDeclaredKeyHoldingAContainerFailsClosed takes it away one
			// level down.
			if level.freeForm {
				found = append(found, sensitiveValuesBeneathAnUndeclaredShape(text, entry.Value)...)
				continue
			}
			found = append(found, sensitiveValuesOfUnknownEntry(text, name, entry.Value)...)
			continue
		}
		found = append(found, sensitiveValuesUnder(text, spec, entry.Value)...)
	}
	return found
}

// sensitiveValuesUnder is what one declared key contributes: the value itself when the key is
// sensitive, and whatever the shape beneath it contributes when the key names one shape.
//
// A key that names no shape describes a scalar leaf, so it says nothing about a container written
// beneath it -- which is the answer that used to be "nothing here is sensitive" for every declared
// public key in the table, and rendered a connection string written under `worker.concurrency`,
// `retention.keep` or `listeners[].name` verbatim.
func sensitiveValuesUnder(text *source, spec keySpec, value ast.Node) []sensitiveValue {
	var found []sensitiveValue

	if spec.sensitive != publicValue {
		found = append(found, locateSensitiveValues(text, value, spec.sensitive)...)
		// Already hidden whole. Descending would add overlapping replacements whose source
		// coordinates no longer describe the copy after the outer one.
		if len(found) != 0 {
			return found
		}
	}

	child, declared := schemaLevels[spec.child]
	if !declared {
		return append(found, sensitiveValuesBeneathAnUndeclaredShape(text, value)...)
	}
	for _, node := range valuesOf(value) {
		found = append(found, sensitiveValues(text, child, node)...)
	}
	return found
}

// valuesOf is what a key's value holds: itself, or every element when it is a sequence.
// `listeners:` holds a list of listener mappings and `secrets:` a list of secrets, and each is one
// key naming one shape however many of that shape it holds.
//
// Stage E's merge keys ask a question that looks the same and is not, so they have their own
// answer (`mergeSourcesOf`) rather than this one. `<<: [id, total]` is not two sources with two
// mistakes in them; it is one value a merge cannot take, and the diagnostic has to say so once.
// A schema key has no such distinction -- `listeners: [1, 2]` is two listeners, each wrong in its
// own right -- which is why this reader splits unconditionally and that one does not.
func valuesOf(value ast.Node) []ast.Node {
	held := beneathNodeProperties(value)
	if sequence, isSequence := held.(*ast.SequenceNode); isSequence {
		return sequence.Values
	}
	return []ast.Node{held}
}
