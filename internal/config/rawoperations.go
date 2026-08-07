package config

import (
	"cmp"
	"slices"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// This file decodes the operations mapping into the contract's own order. The list form is not a case
// here: stage E folded it into the map form, so this reads exactly one shape (AC #2).
//
// **Order is the whole reason it is a slice.** A Go map's iteration order is unspecified, and
// internal/reconcile hashes a listener's trigger-affecting half into a `spec_hash` that decides
// whether a trigger is replaced -- so a map here would make a reconciliation plan differ between
// two runs over one unchanged file. Ordering by the *contract's* declaration order rather than
// by the author's is what makes the two spellings of one operation set the same value:
// `operations: [update, insert]` and `operations: {insert: {}, update: {}}` decode identically.
//
// The order comes from the schema table rather than from a list of the three names here, because that
// table is where the contract's vocabulary is declared and a second copy could disagree with it
// (TestTheCanonicalOrderIsTheContractsOwn).

// The compile-time claim: a mapping decoded by the library's own struct filling would lose both the
// order and the key positions this type exists to keep.
var (
	_ yaml.NodeUnmarshaler = (*rawOperations)(nil)
	_ Positioned           = rawOperations{}
)

// What this decode declines to read. Neither is reachable by a document: stage F refuses a value
// whose shape is not the mapping the contract declares, and refuses a key it cannot read, stopping the
// run in both cases. Both are reached directly by
// TestTheOperationsDecodeRefusesAShapeItCannotReadRatherThanFailing.
// The mapping refusal names its subject rather than reading "expected a mapping" like the two other
// levels', because three variables carrying one text is three chances to raise the wrong one with nothing
// failing -- and the subject is what tells an author which mapping the caret is about
// (TestNoTwoRefusalsShareOneWording).
var (
	mustBeAnOperationsMapping = fault{message: "expected a mapping of operation names"}
	mustBeAnOperationName     = fault{message: "expected an operation name"}
)

// rawOperations is the operations of one listener, in the contract's order.
type rawOperations struct {
	presence
	// values are held unexported because they are wrappers rather than decode targets: this type
	// resolves them itself, as StrList does its elements (resolvewalk.go).
	values []namedOperation
}

// namedOperation is one entry: the statement's name as the author wrote it, and the filters written
// beneath it.
//
// The name is a Str whose node is the *key*, so a rule about the operation anchors on the key rather
// than on the empty filter mapping beside it -- which is where AC #16's and R28's carets belong and
// where the operations fold already put a folded entry's locator (listsugar.go).
type namedOperation struct {
	Name   Str
	Filter rawOperation
}

// UnmarshalYAML reads every entry of the operations mapping and orders them canonically.
func (o *rawOperations) UnmarshalYAML(node ast.Node) error {
	o.began(node)

	entries, isMapping := beneathNodeProperties(node).(*ast.MappingNode)
	if !isMapping {
		o.refuse(mustBeAnOperationsMapping)
		return nil
	}

	o.values = make([]namedOperation, 0, len(entries.Values))
	for _, entry := range entries.Values {
		named, read := o.operationOf(entry)
		if !read {
			continue
		}
		o.values = append(o.values, named)
	}

	slices.SortStableFunc(o.values, func(a, b namedOperation) int {
		return compareOperationNames(a.Name.value, b.Name.value)
	})
	return nil
}

// operationOf is one entry read as a named operation, and whether it could be read as one.
//
// The name is taken through keyTextOf, this package's one answer to what text a key holds in every
// spelling YAML permits for one (valueposition.go), so `insert`, `'insert'` and `!!str insert` are one
// operation rather than three. The filter beneath it is decoded by the library, and it cannot fail:
// every field of rawOperation is a wrapper.
func (o *rawOperations) operationOf(entry *ast.MappingValueNode) (namedOperation, bool) {
	name, readable := keyTextOf(entry.Key)
	if !readable || name == "" {
		o.refuseAt(entry.Key, mustBeAnOperationName)
		return namedOperation{}, false
	}

	named := namedOperation{}
	named.Name.began(entry.Key)
	named.Name.value = name

	if err := yaml.NodeToValue(beneathNodeProperties(entry.Value), &named.Filter); err != nil {
		// Reachable by no document -- stage F declares the filter a mapping and refuses anything else
		// -- and recorded rather than returned for the reason every container refusal is: the
		// guarantee is this stage's rather than the previous one's
		// (TestTheOperationsDecodeRefusesAShapeItCannotReadRatherThanFailing).
		o.refuseAt(entry.Value, mustBeAnOperationsMapping)
		return namedOperation{}, false
	}
	return named, true
}

// resolve extends the shared resolution with each operation's name and filters, which the walk cannot
// reach: they sit in an unexported slice.
func (o *rawOperations) resolve(pass *decodePass) {
	o.presence.resolve(pass)

	for i := range o.values {
		o.values[i].Name.resolve(pass)
		o.values[i].Filter.Columns.resolve(pass)
		o.values[i].Filter.When.resolve(pass)
	}
}

// compareOperationNames orders two operation names as the contract states them -- insert, then update,
// then delete -- and orders a name the contract does not declare after all of them, alphabetically.
//
// An undeclared name is reachable by no document, because R28 refuses one at stage F. Ordering it
// rather than dropping it is what keeps the order *total*: a comparison that answered equal for two
// unknown names would make the sort's output depend on the order the map was walked in, which is the
// dependency this whole file exists to remove.
func compareOperationNames(a, b string) int {
	first, second := declaredOperationIndex(a), declaredOperationIndex(b)
	if first != second {
		return first - second
	}
	return cmp.Compare(a, b)
}

// declaredOperationIndex is a name's place in the contract's own vocabulary, and the number of
// declared names for anything outside it.
func declaredOperationIndex(name string) int {
	declared := schemaLevels[levelOperations].declaredNames()

	if at := slices.Index(declared, name); at >= 0 {
		return at
	}
	return len(declared)
}
