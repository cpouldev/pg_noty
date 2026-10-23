package source

// These assertions check how include_old affects the committed SQL across operations and payload
// modes, independently of the generator used to update the goldens.

import (
	"testing"
)

// theOperationIncludeOldMeansSomethingFor states the include_old rule once, and both directions of
// TestIncludeOldMovesTheCommittedDDLOnlyWhereItIsNotInert read it here: an update is the only
// operation with a pre-change row the flag can add or withhold, because a delete always carries old
// -- it is the only data there is -- and an insert always carries old as null.
const theOperationIncludeOldMeansSomethingFor = "update"

func includeOldIsInertFor(operation string) bool {
	return operation != theOperationIncludeOldMeansSomethingFor
}

// One cell per operation x mode, and include_old is inert for two of the three operations: 2 x 4 =
// 8 of the twelve pairs must be byte-identical and the other 1 x 4 = 4 must differ.
const (
	inertGoldenPairs = 8
	movedGoldenPairs = 4
)

type goldenPair struct{ operation, firstName, secondName string }

// goldenPairsAcrossIncludeOld derives both members of every pair from the crossed axes through
// gridGoldenName, so no pair can be a hand-listed filename that has drifted from the corpus.
func goldenPairsAcrossIncludeOld(t *testing.T) []goldenPair {
	t.Helper()
	if len(generationIncludeOld) != 2 {
		t.Fatalf("the include_old axis holds %d spellings; a pair is defined only for two",
			len(generationIncludeOld))
	}
	var pairs []goldenPair
	for _, operation := range generationOperations {
		for _, mode := range generationModes {
			pair := goldenPair{operation: operation,
				firstName:  gridGoldenName(operation, mode.name, generationIncludeOld[0]),
				secondName: gridGoldenName(operation, mode.name, generationIncludeOld[1])}
			if pair.firstName == pair.secondName {
				t.Fatalf("%s names both include_old spellings of the %s %s cell",
					pair.firstName, operation, mode.name)
			}
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

func TestIncludeOldMovesTheCommittedDDLOnlyWhereItIsNotInert(t *testing.T) {
	inert, moved := 0, 0
	for _, pair := range goldenPairsAcrossIncludeOld(t) {
		first, second := committedGolden(t, pair.firstName), committedGolden(t, pair.secondName)
		if includeOldIsInertFor(pair.operation) {
			inert++
			if first != second {
				t.Errorf("%s and %s differ; include_old is inert for %s, which reads no pre-change row, "+
					"so the flag has leaked into DDL it cannot mean anything for", pair.firstName, pair.secondName, pair.operation)
			}
			continue
		}
		moved++
		if first == second {
			t.Errorf("%s and %s are byte-identical; an %s carries the pre-change row only when the flag "+
				"is set, and a generator emitting an old expression everywhere passes without this",
				pair.firstName, pair.secondName, pair.operation)
		}
	}
	if inert != inertGoldenPairs || moved != movedGoldenPairs {
		t.Fatalf("compared %d inert and %d moved pairs, want %d and %d; a scan reaching only one "+
			"direction proves nothing about the flag's scope", inert, moved, inertGoldenPairs, movedGoldenPairs)
	}
}
