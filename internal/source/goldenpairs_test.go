package source

// The assertions here read the committed corpus as data -- against the axes it is meant to be the
// crossing of, and against itself -- rather than against freshly generated text, which `go test
// -update` rewrites: a regressed grid_insert_*_old_true.golden matches a regressed generator and
// the suite stays green. Split from golden_test.go for the 200-line budget, not by subject.

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// observedGoldenPopulation reads the three populations the pin reconciles: the axis sizes gridCases
// ranges over, the goldens the crossed rows and the named cases ask for, and the files on disk.
func observedGoldenPopulation(t *testing.T) goldenPopulation {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "*.golden"))
	if err != nil {
		t.Fatalf("glob the golden corpus: %v", err)
	}
	found := goldenPopulation{operations: len(generationOperations), modes: len(generationModes), includeOld: len(generationIncludeOld)}
	for _, testCase := range gridCases() {
		found.rows = append(found.rows, testCase.name)
	}
	for _, testCase := range namedGoldenCases() {
		found.named = append(found.named, testCase.name)
	}
	for _, path := range paths {
		found.committed = append(found.committed, filepath.Base(path))
	}
	return found
}

// countIssue is the one wording for a population that has stopped matching its declaration.
func countIssue(subject string, got, want int) string {
	if got == want {
		return ""
	}
	return fmt.Sprintf("%s is %d, want the declared %d", subject, got, want)
}

// goldenPopulationIssues reports every way the corpus stops being the crossed product its axes
// declare. Each clause reads two populations that move independently, so a dropped mode, operation,
// include_old spelling or cell fails by name here rather than in production.
func goldenPopulationIssues(found goldenPopulation) []string {
	var issues []string
	for _, issue := range []string{
		countIssue("the operation axis", found.operations, declaredOperations),
		countIssue("the mode axis", found.modes, declaredModes),
		countIssue("the include_old axis", found.includeOld, declaredIncludeOld),
		countIssue(fmt.Sprintf("the crossing's row count (%d x %d x %d)", declaredOperations,
			declaredModes, declaredIncludeOld), len(found.rows), declaredGridCells),
		countIssue("the named case count", len(found.named), declaredNamedGoldens),
		countIssue(fmt.Sprintf("the committed corpus (%d + %d)", declaredGridCells, declaredNamedGoldens),
			len(found.committed), declaredGridCells+declaredNamedGoldens),
	} {
		if issue != "" {
			issues = append(issues, issue)
		}
	}
	asked := slices.Concat(found.rows, found.named)
	for _, name := range asked {
		if !slices.Contains(found.committed, name) {
			issues = append(issues, name+" is asked for and is committed nowhere under testdata/")
		}
	}
	for _, name := range found.committed {
		if !slices.Contains(asked, name) {
			issues = append(issues, name+" is committed under testdata/ and no crossed row or named case asks for it")
		}
	}
	return issues
}

// droppedGoldenCell is the cell one control removes, derived from the axes so that it names a real
// committed golden however they are later ordered; unaskedGolden is one no crossing produces.
var droppedGoldenCell = gridGoldenName(generationOperations[0], generationModes[0].name,
	generationIncludeOld[len(generationIncludeOld)-1])

const unaskedGolden = "grid_insert_full_old_perhaps.golden"

// Each row is the observed population with one thing changed, fed to goldenPopulationIssues -- the
// function TestGoldenCorpusIsTheCrossedProduct itself calls, not a second expression of the pin. One
// row per clause, because a population tripping two of them cannot say which fired. The guard's
// other side -- that the corpus as committed is refused for nothing -- is that same test, over the
// unmutated population.
var theCorpusPopulationMutations = []struct {
	name   string
	mutate func(goldenPopulation) goldenPopulation
	want   string
}{
	{"a declared cell committed nowhere", func(f goldenPopulation) goldenPopulation {
		f.committed = slices.DeleteFunc(slices.Clone(f.committed),
			func(name string) bool { return name == droppedGoldenCell })
		return f
	}, droppedGoldenCell + " is asked for"},
	{"a golden no crossed row asks for", func(f goldenPopulation) goldenPopulation {
		f.committed = slices.Concat(f.committed, []string{unaskedGolden})
		return f
	}, unaskedGolden + " is committed under testdata/"},
	{"a mode dropped from the axis", func(f goldenPopulation) goldenPopulation { f.modes--; return f },
		"the mode axis is 3, want the declared 4"},
	{"an operation dropped from the axis", func(f goldenPopulation) goldenPopulation { f.operations--; return f },
		"the operation axis is 2, want the declared 3"},
	{"an include_old spelling dropped from the axis", func(f goldenPopulation) goldenPopulation { f.includeOld--; return f },
		"the include_old axis is 1, want the declared 2"},
	{"a crossed row nobody wrote", func(f goldenPopulation) goldenPopulation {
		f.rows = f.rows[:len(f.rows)-1]
		return f
	}, "the crossing's row count (3 x 4 x 2) is 23, want the declared 24"},
}

func TestTheCorpusPinFailsOnADroppedCellOrAxisValue(t *testing.T) {
	whole := observedGoldenPopulation(t)
	for _, mutation := range theCorpusPopulationMutations {
		t.Run(mutation.name, func(t *testing.T) {
			issues := goldenPopulationIssues(mutation.mutate(whole))
			if !slices.ContainsFunc(issues, func(issue string) bool {
				return strings.Contains(issue, mutation.want)
			}) {
				t.Fatalf("goldenPopulationIssues reported %v, want an issue containing %q", issues, mutation.want)
			}
		})
	}
}

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
			// Unreachable while gridGoldenName spells the flag: a builder dropping it fails
			// TestGoldenCorpusIsTheCrossedProduct first, on twelve goldens asked for by nobody.
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
