package config

import (
	"fmt"
	"math/bits"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// The operations list sugar: `operations: [insert, update]` reduced to the map form, so the
// structural pass meets one shape (AC #2). What the map form then has to satisfy is stage F's
// question and deliberately not this one. What the fold declines is operationrefusal_test.go's.

// filterFree is the decoded shape a folded operation must take: named, and carrying nothing.
func filterFree(names ...string) map[string]any {
	operations := make(map[string]any, len(names))
	for _, name := range names {
		operations[name] = map[string]any{}
	}
	return operations
}

// assertFolds is the outcome the fold's cases share, so each states only its own input and its
// own expectation.
func assertFolds(t *testing.T, document string, want map[string]any) {
	t.Helper()

	if folded := decodedDocument(t, normalized(t, document))["operations"]; !reflect.DeepEqual(folded, want) {
		t.Errorf("operations = %#v, want %#v", folded, want)
	}
}

// TestTheOperationsListFoldsIntoAMappingOfFilterFreeOperations is SC-5's main case.
func TestTheOperationsListFoldsIntoAMappingOfFilterFreeOperations(t *testing.T) {
	assertFolds(t, "operations: [insert, update]\n", filterFree("insert", "update"))
}

// TestAOneElementOperationsListFoldsToASingleEntryMapping is the lower boundary of a list that
// still holds something.
func TestAOneElementOperationsListFoldsToASingleEntryMapping(t *testing.T) {
	assertFolds(t, "operations: [insert]\n", filterFree("insert"))
}

// TestAnEmptyOperationsListFoldsToAnEmptyMapping is the other boundary, and the reduction has to
// happen there too: an empty list is a listener with no operations, which R27 refuses against a
// mapping with no entries -- not here, against a shape stage F would need a second rule for.
func TestAnEmptyOperationsListFoldsToAnEmptyMapping(t *testing.T) {
	_, root, diags := stageE(t, "operations: []\n", nil)

	if len(diags) != 0 {
		t.Fatalf("an empty list was reported here rather than left to stage F: %q", messagesOf(diags))
	}
	folded, isMapping := valueOfEntry(root, 0).(*ast.MappingNode)
	if !isMapping {
		t.Fatalf("operations is %T, want an empty mapping", valueOfEntry(root, 0))
	}
	if len(folded.Values) != 0 {
		t.Errorf("the folded mapping holds %d entries, want none", len(folded.Values))
	}
}

// operationsListSpelling is one way a document can write the list form, as a template over what
// the list holds: the single `%s` of document is where the operation names go, written in that
// row's own style.
type operationsListSpelling struct {
	document string
	// block says the row writes a block sequence, whose elements are one indented `- name` line
	// each rather than a comma-separated list inside brackets.
	block bool
	// locator is where the value the fold replaces reports itself, which is the locator the built
	// mapping has to inherit. It is written per row rather than defaulted, because the two answers
	// are different rules and a row that forgot to choose would silently take one of them: a list
	// written in place is at `$.operations`, while a list reached through an alias keeps the
	// *anchor's* own locator, which Implementation Note 6 forbids a later stage re-pathing.
	locator string
}

// written is this spelling holding these operations.
func (s operationsListSpelling) written(names []string) string {
	if !s.block {
		return fmt.Sprintf(s.document, strings.Join(names, ", "))
	}

	var lines strings.Builder
	for _, name := range names {
		lines.WriteString("  - " + name + "\n")
	}
	return fmt.Sprintf(s.document, lines.String())
}

// operationsListSpellings is every way a document can write the list form: the sequence itself,
// and each node property a value position permits around it
// (.claude/rules/accept-every-node-property-the-position-admits.md).
//
// The alias rows also pin the order the two reductions run in: a value is reduced before the fold
// is offered it, so `operations: *ops` arrives as the sequence it names rather than leaving the
// list form standing for a stage that was promised the map form.
var operationsListSpellings = map[string]operationsListSpelling{
	"a flow sequence":               {document: "operations: [%s]\n", locator: "$.operations"},
	"a block sequence":              {document: "operations:\n%s", block: true, locator: "$.operations"},
	"a tagged sequence":             {document: "operations: !!seq [%s]\n", locator: "$.operations"},
	"an anchored sequence":          {document: "operations: &ops [%s]\n", locator: "$.operations"},
	"an anchored tagged sequence":   {document: "operations: &ops !!seq [%s]\n", locator: "$.operations"},
	"an alias to a sequence":        {document: "ops: &ops [%s]\noperations: *ops\n", locator: "$.ops"},
	"an alias to a tagged sequence": {document: "ops: &ops !!seq [%s]\noperations: *ops\n", locator: "$.ops"},
}

// operationsListLengths is the other dimension: how many operations the list holds. The three
// boundaries of a list are all here, because the fold's per-element loop is what the node
// properties are read off *for* and a property row exercised at one length only leaves the two
// dimensions crossed nowhere.
var operationsListLengths = map[string][]string{
	"holding two operations": {"insert", "update"},
	"holding one operation":  {"insert"},
	"holding none":           {},
}

// TestEveryNodePropertyAnOperationsListAdmitsIsFolded closes the fold over the grammar of the
// position it reads rather than over the spelling the other cases happen to write. A spelling
// that escapes it is not a spelling left alone: it reaches stage F as a sequence, which is the
// one thing this stage exists to make impossible.
//
// The properties are read off rather than carried through, because the folded value is a mapping
// -- `!!seq` on it would be a false claim about a node the fold built.
//
// One cell of the crossing is skipped and cannot be written: YAML has no empty block sequence.
// `operations:` with nothing beneath it is the null value, which is a different document with a
// different answer -- stage F's wrong-node-kind check, not a fold.
func TestEveryNodePropertyAnOperationsListAdmitsIsFolded(t *testing.T) {
	for spelling, written := range operationsListSpellings {
		for length, names := range operationsListLengths {
			if written.block && len(names) == 0 {
				continue
			}

			t.Run(spelling+" "+length, func(t *testing.T) {
				assertFolds(t, written.written(names), filterFree(names...))
			})
		}
	}
}

// TestABareOperationsScalarIsLeftForTheStructuralPass draws this step's boundary. `operations:
// insert` is neither form, and folding it would answer AC #16's bare-scalar case with a silent
// acceptance instead of the diagnostic naming both accepted forms.
func TestABareOperationsScalarIsLeftForTheStructuralPass(t *testing.T) {
	_, root, diags := stageE(t, "operations: insert\n", nil)

	if len(diags) != 0 {
		t.Fatalf("the bare scalar was reported here rather than left to stage F: %q", messagesOf(diags))
	}
	if _, isString := valueOfEntry(root, 0).(*ast.StringNode); !isString {
		t.Errorf("operations became %T; a bare scalar must reach stage F as written", valueOfEntry(root, 0))
	}
}

// TestTheFoldedMapIsIndistinguishableFromTheHandWrittenMapForm is AC #2's equivalence claim as
// a decoded value: the two spellings a contributor may write produce one document.
func TestTheFoldedMapIsIndistinguishableFromTheHandWrittenMapForm(t *testing.T) {
	written := decodedDocument(t, normalized(t, "operations:\n  insert: {}\n  update: {}\n"))

	assertFolds(t, "operations: [insert, update]\n", written["operations"].(map[string]any))
}

// TestOperationsIsTheOnlyKeyTheContractAcceptsInTwoShapes scopes the fold to the one key it was
// reasoned about (.claude/rules/assert-a-set-wide-invariant-over-the-set.md). "A list of names
// stands for a mapping of them to empty filters" is true of operations and of nothing else, so a
// second key declared in two shapes needs its own decision rather than this one by default.
func TestOperationsIsTheOnlyKeyTheContractAcceptsInTwoShapes(t *testing.T) {
	var polymorphic []string
	for _, level := range schemaLevels {
		for _, spec := range level.keys {
			if bits.OnesCount8(uint8(spec.kinds)) > 1 {
				polymorphic = append(polymorphic, spec.name)
			}
		}
	}

	slices.Sort(polymorphic)
	if !slices.Equal(polymorphic, []string{operationsKey}) {
		t.Errorf("the contract accepts %v in more than one shape, want only %q; the fold covers one key",
			polymorphic, operationsKey)
	}
}
