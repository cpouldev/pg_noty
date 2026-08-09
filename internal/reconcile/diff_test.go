package reconcile

import (
	"strings"
	"testing"
)

func TestAnEqualSpecHashDoesNotSkipTheCatalogComparison(t *testing.T) {
	intact := ownedPairSource(t, Pair{Listener: "orders", Operation: "insert"})
	intact.Desired.SpecificationChanged = true
	missing := intact
	missing.Observed = observedPair{Pair: intact.pair()}
	// Both take the equal-hash desired/recorded short circuit; catalog presence alone differs.
	// pin-documented-precedence.md makes the missing neighbour prove catalog comparison still runs.
	clean := diff(diffInputFor(intact))
	recreated := diff(diffInputFor(missing))
	if clean.Verdict != VerdictClean || clean.Examined != 1 || len(clean.Actions) != 0 {
		t.Fatalf("catalog-intact equal-hash plan = %+v, want one examined clean pair", clean)
	}
	if recreated.Verdict != VerdictChangesPending || recreated.Examined != 1 || len(recreated.Actions) != 1 {
		t.Fatalf("catalog-missing equal-hash plan = %+v, want one recreate", recreated)
	}
	assertActionKind(t, recreated.Actions[0], missing.pair(), ActionCreate)
}

func TestAnUnequalSpecHashClassifiesReplacementWithoutACallerFlag(t *testing.T) {
	source := ownedPairSource(t, Pair{Listener: "orders", Operation: "insert"})
	source.Desired.SpecHash, source.Desired.SpecificationChanged = "changed", false
	result := diff(diffInputFor(source))
	if len(result.Actions) != 1 {
		t.Fatalf("unequal-hash diff = %+v, want one replacement", result)
	}
	assertActionKind(t, result.Actions[0], source.pair(), ActionReplace)
}

func TestClassificationNearMissPairsCannotAnswerAlike(t *testing.T) {
	// test-both-sides-of-an-exclusion-guard.md requires each adjacent input to reject the other's result.
	retained := ownedPairSource(t, Pair{Listener: "orders", Operation: "insert"})
	retained.Desired.OperationPresent = false
	removed := retained
	// Only listener retention differs: retained has no operations; removed has no listener.
	removed.Desired.ListenerPresent = false
	retainedResult, removedResult := classifyPair(retained), classifyPair(removed)
	assertClassifiedAction(t, retainedResult, retained.pair(), ActionDrop)
	assertClassifiedAction(t, removedResult, removed.pair(), ActionDrop)
	if !retainedResult.RetainListener || removedResult.RetainListener {
		t.Fatalf("retained=%+v removed=%+v, want distinct listener retention", retainedResult, removedResult)
	}

	missing := ownedPairSource(t, Pair{Listener: "orders", Operation: "insert"})
	wrong := missing
	missing.Observed = observedPair{Pair: missing.pair()}
	// The one discriminating property is the catalog observation: absent versus present-and-wrong.
	wrong.Observed.Trigger.Definition = "hand-edited trigger"
	missingResult, wrongResult := classifyPair(missing), classifyPair(wrong)
	assertClassifiedAction(t, missingResult, missing.pair(), ActionCreate)
	assertClassifiedAction(t, wrongResult, wrong.pair(), ActionReplace)
	if missingResult.Action.Kind == wrongResult.Action.Kind {
		t.Fatal("criteria 21 and 22 answered alike; each input must fail the other's expectation")
	}
}

func TestThreeOperationsBindEachVerdictToItsOwnPair(t *testing.T) {
	// assert-a-position-per-element-not-as-a-set.md requires one verdict assertion per operation.
	cases := []struct {
		operation string
		changed   bool
		want      ActionKind
	}{
		{operation: "insert"},
		{operation: "update", changed: true, want: ActionReplace},
		{operation: "delete"},
	}
	input := diffInput{Instance: "alpha"}
	for _, testCase := range cases {
		source := ownedPairSource(t, Pair{Listener: "orders", Operation: testCase.operation})
		if testCase.changed {
			source.Desired.SpecHash = "changed"
		}
		input.Desired = append(input.Desired, source.Desired)
		input.Recorded = append(input.Recorded, source.Recorded)
		input.Observed = append(input.Observed, source.Observed)
	}
	result := diff(input)
	if result.Examined != len(cases) || result.Refusals != nil {
		t.Fatalf("three-operation diff = %+v, want three examined verdicts", result)
	}
	for _, testCase := range cases {
		pair := Pair{Listener: "orders", Operation: testCase.operation}
		actions := actionsForPair(result.Actions, pair)
		if testCase.want == "" && len(actions) != 0 {
			t.Fatalf("pair %+v actions = %#v, want its own clean verdict", pair, actions)
		}
		if testCase.want != "" && len(actions) == 1 {
			assertActionKind(t, actions[0], pair, testCase.want)
		} else if testCase.want != "" {
			t.Fatalf("pair %+v actions = %#v, want one %s", pair, actions, testCase.want)
		}
	}
}

func TestDiffExaminesEveryPairNamedByEitherSource(t *testing.T) {
	applied := ownedPairSource(t, Pair{Listener: "orders", Operation: "insert"})
	stale := ownedPairSource(t, Pair{Listener: "retired", Operation: "delete"})
	input := diffInputFor(applied)
	input.Recorded = append(input.Recorded, stale.Recorded)
	input.Observed = append(input.Observed, stale.Observed)
	result := diff(input)
	wantExamined := len(namedPairs(input))
	if result.Examined != wantExamined || len(result.Actions) != 1 {
		t.Fatalf("diff = %+v, want %d examined pairs and one registry-only drop", result, wantExamined)
	}
	assertActionKind(t, result.Actions[0], stale.pair(), ActionDrop)
}

func TestDiffExaminesACatalogOnlyPair(t *testing.T) {
	stray := ownedPairSource(t, Pair{Listener: "stray", Operation: "update"})
	input := diffInput{Observed: []observedPair{stray.Observed}, Instance: stray.Instance}
	result := diff(input)
	if result.Examined != len(namedPairs(input)) || len(result.Refusals) != 1 || len(result.Actions) != 0 {
		t.Fatalf("catalog-only diff = %+v, want one examined ownership refusal and no action", result)
	}
}

func diffInputFor(source pairSources) diffInput {
	input := diffInput{Recorded: []recordedPair{source.Recorded}, Observed: []observedPair{source.Observed}, Instance: source.Instance}
	if source.Desired.ListenerPresent {
		input.Desired = []desiredPair{source.Desired}
		input.Listeners = []desiredListener{{Name: source.Desired.Pair.Listener, SpecHash: source.Desired.SpecHash, Enabled: source.Desired.Enabled}}
	}
	return input
}

func namedPairs(input diffInput) map[Pair]bool {
	pairs := map[Pair]bool{}
	for _, source := range input.Desired {
		pairs[source.Pair] = true
	}
	for _, source := range input.Recorded {
		pairs[source.Pair] = true
	}
	for _, source := range input.Observed {
		pairs[source.Pair] = true
	}
	return pairs
}

func assertClassifiedAction(t *testing.T, got pairClassification, pair Pair, want ActionKind) {
	t.Helper()
	if got.Refusal != nil || got.Action == nil {
		t.Fatalf("classification = %+v, want %s for %+v", got, want, pair)
	}
	assertActionKind(t, *got.Action, pair, want)
}

func assertActionKind(t *testing.T, got Action, pair Pair, want ActionKind) {
	t.Helper()
	if got.Pair != pair || got.Kind != want {
		t.Fatalf("action = %+v, want %s bound to %+v", got, want, pair)
	}
}

func actionsForPair(actions []Action, pair Pair) []Action {
	var matched []Action
	for _, action := range actions {
		if action.Pair == pair {
			matched = append(matched, action)
		}
	}
	return matched
}

func TestClassifyIsTheOnlyCatalogAndRegistryConversionSeam(t *testing.T) {
	// reuse-the-helper-before-copying-it.md keeps the L1 ownership proof free of adapter readings.
	var catalogConversions, registryConversions []string
	for _, name := range productionSourceNames(t) {
		text := string(sourceBytes(t, name))
		if strings.Contains(text, "CatalogObject{") {
			catalogConversions = append(catalogConversions, name)
		}
		if strings.Contains(text, "RegistryRow{") {
			registryConversions = append(registryConversions, name)
		}
	}
	if len(catalogConversions) != 1 || catalogConversions[0] != "classify.go" || len(registryConversions) != 1 || registryConversions[0] != "classify.go" {
		t.Fatalf("CatalogObject conversions=%v RegistryRow conversions=%v, want classify.go only", catalogConversions, registryConversions)
	}
}
