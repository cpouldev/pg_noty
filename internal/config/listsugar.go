package config

import (
	"strconv"

	"github.com/goccy/go-yaml/ast"
)

// This file folds `operations: [insert, update]` into the map form the contract states, so the
// structural pass meets exactly one shape (AC #2). It reduces a spelling and judges nothing:
// whether the names are operations the product supports, and whether there is at least one of
// them, are stage F's and stage H's questions about the mapping this leaves behind.

// bothOperationForms shows an author the two spellings the contract accepts. It is one string
// because two diagnostics have to show both forms -- this stage's refusal of a list element that
// cannot become a name, and stage F's refusal of a value that is neither form (AC #16) -- and a
// second copy would drift the moment either spelling changed. It is declared here, beside the fold
// that reduces one form to the other, and the schema table names it at the key that accepts both.
const bothOperationForms = "write operations: [insert, update], or use the map form operations: {update: {...}}"

// What the fold declines to do, for the one reason that is its own. The other two are
// keyreference.go's, because an objection to a key is the same objection whichever stage meets
// it (AC #6): a reader must not be able to tell which stage found a key holding `${`.
var unnameableOperation = fault{
	message: "an operations list entry must be a name",
	hint:    bothOperationForms,
}

// repeatedOperation is what the second of two list entries naming one operation is told. It names
// the first occurrence's line, which is the convention the parser's duplicate-key diagnostic sets at
// stage B and stage F's two answers to the same condition follow (anotherSpellingOfOneKey,
// headerWrittenTwice), so one condition reads one way however an author wrote it.
func repeatedOperation(first int) fault {
	return fault{
		message: "this entry names the operation already written on line " + strconv.Itoa(first),
		hint:    "write each operation once; a list entry stands for one operation with no filter",
	}
}

// foldedOperations is what one entry's value becomes: the mapping a list of operation names
// stands for, or the value itself. The caller installs the answer, so the rewrite is visible
// where it happens rather than inside a function that returns nothing.
//
// Two shapes are deliberately left alone. The map form is already the shape everything after
// this expects, and a bare scalar is neither form: folding `operations: insert` would answer
// AC #16's diagnostic -- the one that shows an author both accepted forms -- with a silent
// acceptance.
//
// The entry is found by its key name rather than by where the schema says the key belongs,
// because this stage reduces spellings and does not judge placement: a walk that knew which
// level it was at would be stage F's walk, run early. Folding an `operations` key written
// somewhere the contract does not declare one changes no verdict -- the key is unknown there
// whichever shape its value has -- so the imprecision costs a diagnostic's wording at most.
func (nz *normalizer) foldedOperations(key ast.MapKeyNode, value ast.Node) ast.Node {
	if name, readable := keyTextOf(key); !readable || name != operationsKey {
		return value
	}

	// The properties an author may write on this value are read off before the list shape is
	// asserted: `operations: !!seq [insert]` is legal YAML, and a position that asserted the
	// shape directly would hand stage F the sequence this stage exists to remove. They are read
	// off rather than carried through, because what the fold returns is a mapping and `!!seq`
	// on it would be a false claim about a node this stage built.
	written, isList := beneathNodeProperties(value).(*ast.SequenceNode)
	if !isList {
		return value
	}

	folded, complete := nz.operationEntriesOf(written)
	if !complete {
		// A refused list keeps the shape its author wrote, for the reason stage D leaves a
		// refused scalar alone: the run stops, so nothing reads the value and the only thing a
		// later reader could find here is what was written.
		return value
	}

	// Both halves of the node this replaces are carried across: its token, so the position is the
	// rune the author typed, and its locator, so a diagnostic about the mapping names something.
	// ast.Mapping sets a bare BaseNode, and stage F anchors R27's "at least one entry" on exactly
	// this mapping -- an empty `Path` there would name nothing, and, because `Path` sits inside
	// both the diagnostic sort key and prefersOver, would also sort ahead of every other
	// diagnostic in the run.
	//
	// The locator comes from `value` rather than from `written`, so it is the locator of the node
	// the document actually held here: for `!!seq [insert]` that is the tag node, and for
	// `operations: *ops` it is the anchored list, which keeps the anchor's own locator per
	// Implementation Note 6.
	mapping := ast.Mapping(written.GetToken(), written.IsFlowStyle, folded...)
	mapping.SetPath(value.GetPath())
	return mapping
}

// operationEntriesOf is one mapping entry per element of the list, and whether every element
// became one.
//
// Every element is examined before the fold is abandoned, because this stage's policy is to
// accumulate and then stop: a list holding two unusable elements names both in one run rather
// than sending its author back for the second after they have fixed the first.
//
// Slice order is document order here, unlike a mapping's: a sequence's elements are the ones its
// author wrote and no stage rewrites them, so the line an operation was first named on is the
// earlier element's. Stage E does rewrite a mapping's entries, which is why the same question there
// is re-derived from position.
func (nz *normalizer) operationEntriesOf(written *ast.SequenceNode) ([]*ast.MappingValueNode, bool) {
	folded := make([]*ast.MappingValueNode, 0, len(written.Values))
	namedOn := make(map[string]int, len(written.Values))
	complete := true

	for _, element := range written.Values {
		if alreadyRefusedAlias(element) {
			// The fold adds no second complaint about it, and it still costs the fold: the
			// element has no name to become.
			complete = false
			continue
		}

		operation, why, refused := operationKeyOf(element)
		if refused {
			nz.report(element, why)
			complete = false
			continue
		}

		identity := keyIdentity(operation)
		if first, already := namedOn[identity]; already {
			// Two elements naming one operation would fold to a mapping holding one key twice --
			// and the parser's own duplicate-key detection (R42) ran at stage B, over the keys
			// the author wrote. This key is not one of those: the fold invents it here, after
			// that detection. Refusing is what keeps "no mapping reaching a later stage holds a
			// key twice" true of the mappings this stage builds, rather than leaving the decode
			// to fail over a key no diagnostic ever named.
			nz.report(element, repeatedOperation(first))
			complete = false
			continue
		}
		namedOn[identity] = positionOf(nz.src, operation).Line()

		folded = append(folded, filterFreeOperation(operation))
	}
	return folded, complete
}

// operationKeyOf is the key one list element becomes, or the reason it cannot become one and the
// fact that there is one. Presence is its own result rather than an empty message field, so a
// fault that happened to say nothing could not read as the absence of one.
//
// The second reason is the one this stage owes AC #6, and it is asked of keyreference.go rather
// than encoded here. A list element is a value, so stage D substituted into it, and its bytes are
// opaque by design -- so a reference the environment supplied was never scanned as a key, and
// folding would write it into a key position after the only check that looks at keys has run.
// That check runs again here, on the keys this stage introduces and on nothing else.
//
// One of the two answers it can give is reached by no document: an alias is the one key shape
// keyTextOf declines, and the caller answers an alias element before asking this. It is reached
// instead by TestAnIntroducedKeyNobodyCanReadIsRefusedRatherThanFolded, which hands this function
// that shape directly.
func operationKeyOf(element ast.Node) (named ast.MapKeyNode, why fault, refused bool) {
	key, canBeAKey := element.(ast.MapKeyNode)
	if !canBeAKey {
		return nil, unnameableOperation, true
	}

	if why, holdsOne := keyReferenceFault(key); holdsOne {
		return nil, why, true
	}
	return key, fault{}, false
}

// filterFreeOperation is one folded entry: the element itself standing as the key, and an empty
// mapping standing for the filter the list form declines to write.
//
// Every node it builds carries the element's own token, so a later diagnostic about this
// operation points at the rune the author typed rather than at a position invented here. The
// filter is an empty *flow* mapping because that is what `insert: {}` parses to, which is what
// makes the two forms indistinguishable once decoded.
//
// The filter and the entry are given the element's own locator too. Neither ast.Mapping nor
// ast.MappingValue sets one, and a node with no path renders as the empty string -- so a Step-5 or
// Step-8 diagnostic about a folded operation would carry no locator at all. `$.operations[0]` still
// reads as a list index rather than as `$.operations.insert` (Implementation Note 15), which is the
// element's honest locator: it is where the author wrote it. Step 5 answered that note's open
// question by keeping it, and by anchoring every per-operation diagnostic on the key rather than on
// the filter.
func filterFreeOperation(named ast.MapKeyNode) *ast.MappingValueNode {
	at, locator := named.GetToken(), named.GetPath()

	filter := ast.Mapping(at, true)
	filter.SetPath(locator)

	entry := ast.MappingValue(at, named, filter)
	entry.SetPath(locator)
	return entry
}
