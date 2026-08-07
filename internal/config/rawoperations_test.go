package config

import (
	"reflect"
	"slices"
	"testing"
)

// What the operations mapping decodes to: the contract's own order, each statement's name positioned on
// the key the author wrote, and a shape it cannot read refused rather than returned.

// TestTheOperationSetDecodesIntoTheContractsOrder is the determinism claim. It is asserted from the two
// orders an author can write and from both spellings the contract accepts, because the value has to be
// the same in all of them: AC #2 makes the list form equivalent to a filter-free map, and phase 4 hashes
// this value into a spec_hash that must not change when a file is reordered.
func TestTheOperationSetDecodesIntoTheContractsOrder(t *testing.T) {
	tests := map[string]string{
		"the map form written in the contract's order": "    operations:\n      insert: {}\n" +
			"      update: {}\n      delete: {}\n",
		"the map form written backwards": "    operations:\n      delete: {}\n" +
			"      update: {}\n      insert: {}\n",
		"the list form written backwards": "    operations: [delete, update, insert]\n",
	}
	want := []string{"insert", "update", "delete"}

	for name, written := range tests {
		t.Run(name, func(t *testing.T) {
			decoded := oneDecodedListener(t, operationsOf(written))

			if got := operationNamesOf(decoded.Operations); !slices.Equal(got, want) {
				t.Errorf("decoded %v, want %v", got, want)
			}
		})
	}
}

// TestTheDecodedOperationSetIsASliceRatherThanAMap is the structural half of the determinism claim, and
// the order assertions above cannot make it: a decode into a Go map would satisfy every one of them on a
// three-entry mapping and then answer a different order on a run whose hash seed differed. What must not
// exist is the map, so the type is what gets asserted.
//
// config_test.go makes the same assertion of the public Operations. Both are needed and neither implies
// the other: this is the value phase 4 hashes, and that is the value a consumer reads.
func TestTheDecodedOperationSetIsASliceRatherThanAMap(t *testing.T) {
	held := reflect.TypeFor[rawOperations]()

	values, found := held.FieldByName("values")
	if !found {
		t.Fatal("rawOperations has no values field, so this assertion no longer names the decoded set")
	}
	if values.Type.Kind() != reflect.Slice {
		t.Errorf("rawOperations.values is a %s, want a slice; a map would make the decoded order -- and "+
			"every spec_hash derived from it -- depend on iteration order", values.Type.Kind())
	}
	if got := values.Type.Elem(); got != reflect.TypeFor[namedOperation]() {
		t.Errorf("rawOperations.values holds %s, want namedOperation", got)
	}
}

// TestTheCanonicalOrderIsTheContractsOwn pins where the order comes from. Reading it from the schema
// table is what makes "insert < update < delete" one declaration rather than two that can disagree, so a
// table reordered without this being noticed has to fail here (.claude/rules/pin-documented-precedence.md).
func TestTheCanonicalOrderIsTheContractsOwn(t *testing.T) {
	want := []string{"insert", "update", "delete"}

	if got := schemaLevels[levelOperations].declaredNames(); !slices.Equal(got, want) {
		t.Fatalf("the table declares %v; the canonical order the decode sorts by is read from it, so "+
			"reordering the table reorders every decoded operations set and every spec_hash with it", got)
	}

	// Observe the dependency rather than only checking both sides against the same current
	// order. A comparator with its own hard-coded copy stays unchanged here and fails.
	func() {
		original := schemaLevels[levelOperations]
		reordered := original
		reordered.keys = slices.Clone(original.keys)
		slices.Reverse(reordered.keys)
		schemaLevels[levelOperations] = reordered
		defer func() { schemaLevels[levelOperations] = original }()

		if compareOperationNames("delete", "update") >= 0 ||
			compareOperationNames("update", "insert") >= 0 {
			t.Error("reordering the schema table did not reorder the operation comparator")
		}
	}()

	for i, earlier := range want[:len(want)-1] {
		if compareOperationNames(earlier, want[i+1]) >= 0 {
			t.Errorf("%q does not sort before %q", earlier, want[i+1])
		}
	}
}

// TestAnUndeclaredOperationNameSortsLastAndTotally is the fail-closed half of that order. R28 refuses a
// fourth name at stage F, so no document reaches it -- but an ordering that answered "equal" for two
// names it does not know would make the sort's output depend on the order the mapping was walked in,
// which is the one dependency this decode exists to remove.
func TestAnUndeclaredOperationNameSortsLastAndTotally(t *testing.T) {
	if compareOperationNames("truncate", "insert") <= 0 {
		t.Error("an undeclared name sorts before a declared one")
	}
	if compareOperationNames("truncate", "merge") <= 0 || compareOperationNames("merge", "truncate") >= 0 {
		t.Error("two undeclared names compare equal, so their order would be the walk's")
	}
}

// operationNamesOf is the statement names of a decoded operations set, in the order it holds them.
func operationNamesOf(held rawOperations) []string {
	names := make([]string, 0, len(held.values))
	for _, named := range held.values {
		names = append(names, named.Name.value)
	}
	return names
}
