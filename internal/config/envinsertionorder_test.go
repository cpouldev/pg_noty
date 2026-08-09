package config

import (
	"maps"
	"slices"
	"testing"
)

// This file is the proof that the determinism gate's retired environment dimension was never one.
//
// The gate built its environment twice, shuffling the order the names were inserted in, and compared
// the two runs. The shuffle is gone; what stands in its place is a claim about the representation the
// shuffle actually read, rather than about the corpus it happened to be wired into.

// TestMapEnvCannotObserveTheInsertionOrderOfItsMap is what the gate above gives up by no longer
// varying one, and it is the proof that the dimension it varied was never a dimension.
//
// The gate used to build its environment twice, shuffling the order the names were inserted in, and
// compare the two runs. Nothing could have differed: MapEnv clones the map and closes over a single
// point query, so no consumer holds anything it could range over, and Go randomises map iteration per
// range in any case. A varied input that no code path can observe is a generated dimension that reads
// as coverage and reaches nothing.
//
// The claim is proved against the representation the retired shuffle actually read -- insertion order
// of the map handed to MapEnv -- rather than against the corpus it happened to be wired into.
func TestMapEnvCannotObserveTheInsertionOrderOfItsMap(t *testing.T) {
	names := slices.Sorted(maps.Keys(corpusVariables))
	if len(names) < 2 {
		t.Fatalf("the corpus declares %d variables; two orders need two names", len(names))
	}

	forwards := make(map[string]string, len(names))
	for _, name := range names {
		forwards[name] = corpusVariables[name]
	}
	backwards := make(map[string]string, len(names))
	for index := len(names) - 1; index >= 0; index-- {
		backwards[names[index]] = corpusVariables[names[index]]
	}

	first, second := MapEnv(forwards), MapEnv(backwards)
	for _, name := range append(slices.Clone(names), "A-NAME-NEITHER-MAP-HOLDS") {
		wanted, wantFound := first(name)
		got, found := second(name)
		if got != wanted || found != wantFound {
			t.Errorf("looking up %q gives (%q, %t) in one insertion order and (%q, %t) in the "+
				"other; MapEnv would then have a dimension the retired shuffle could vary",
				name, wanted, wantFound, got, found)
		}
	}
}
