package reconcile

import (
	"strconv"
	"strings"
	"testing"
)

const classificationMatrixCount = 15

var matrixNames = [...]string{
	"new listener", "matching listener", "changed specification", "added operation", "removed operation",
	"no operations retained", "listener removed", "empty listener list", "disabled listener", "stably disabled listener",
	"missing catalog trigger", "wrong catalog definition", "dropped target", "renamed target", "dropped and recreated target",
}

// matrixExpectation is one matrix row's fixture and the verdict it must produce. A row named for a
// property of a whole listener supplies scope instead of source: criteria 15-18 turn on how many of a
// listener's pairs the configuration still declares, which one pairSources cannot express.
type matrixExpectation struct {
	source  pairSources
	scope   listenerScope
	action  ActionKind
	refusal bool
	retain  bool
}

func TestClassificationMatrix(t *testing.T) {
	// a-coverage-registry-names-the-assertion-not-a-test.md requires the matrix to name evidence.
	// assert-a-set-wide-invariant-over-the-set.md and partition-named-test-cases.md close its identity set.
	if got := len(matrixRows); got != classificationMatrixCount {
		t.Fatalf("classification matrix has %d rows, want %d", got, classificationMatrixCount)
	}
	if got := len(matrixNames); got != classificationMatrixCount {
		t.Fatalf("matrix identity set has %d names, want %d", got, classificationMatrixCount)
	}
	for index, row := range matrixRows {
		row := row
		assertMatrixRowIdentity(t, index, row)
		t.Run(strings.TrimPrefix(row.Assertion, "TestClassificationMatrix/"), func(t *testing.T) {
			want := matrixExpectationFor(t, row.Criterion)
			if len(want.scope.outcomes) != 0 {
				assertListenerScope(t, want.scope)
				return
			}
			assertClassification(t, classifyPair(want.source), want)
		})
	}
}

func assertMatrixRowIdentity(t *testing.T, index int, row matrixRow) {
	t.Helper()
	wantCriterion := 11 + index
	wantAssertion := "TestClassificationMatrix/criterion_" + strconv.Itoa(wantCriterion) + "_" + strings.ReplaceAll(matrixNames[index], " ", "_")
	if row.Criterion != wantCriterion || row.Name != matrixNames[index] || row.Assertion != wantAssertion {
		t.Fatalf("matrix row %q at index %d is miswired: %+v, want criterion %d name %q assertion %q", row.Name, index, row, wantCriterion, matrixNames[index], wantAssertion)
	}
}

func matrixExpectationFor(t *testing.T, criterion int) matrixExpectation {
	source := ownedPairSource(t, Pair{Listener: "orders", Operation: "insert"})
	switch criterion {
	case 11:
		source.Recorded, source.Observed = recordedPair{Pair: source.pair()}, observedPair{Pair: source.pair()}
		return matrixExpectation{source: source, action: ActionCreate, retain: true}
	case 12:
		return matrixExpectation{source: source}
	case 13:
		source.Desired.SpecificationChanged, source.Desired.SpecHash = true, "changed"
		return matrixExpectation{source: source, action: ActionReplace, retain: true}
	case 14:
		source.Recorded.TriggerPresent, source.Observed.TriggerPresent, source.Observed.FunctionPresent = false, false, false
		return matrixExpectation{source: source, action: ActionCreate, retain: true}
	// Criteria 15-18 vary a listener rather than a pair, so each supplies its own whole-listener
	// fixture; writing them as one pair made 15 and 16 the same input, and 17 and 18 likewise.
	case 15:
		return matrixExpectation{scope: removedOperationScope(t)}
	case 16:
		return matrixExpectation{scope: noOperationsRetainedScope(t)}
	case 17:
		return matrixExpectation{scope: listenerRemovedScope(t)}
	case 18:
		return matrixExpectation{scope: emptyListenerListScope(t)}
	case 19:
		source.Desired.Enabled = false
		return matrixExpectation{source: source, action: ActionDisable, retain: true}
	case 20:
		source.Desired.Enabled, source.Recorded.TriggerPresent = false, false
		source.Observed = observedPair{Pair: source.pair()}
		return matrixExpectation{source: source}
	case 21:
		source.Observed = observedPair{Pair: source.pair()}
		return matrixExpectation{source: source, action: ActionCreate, retain: true}
	case 22:
		source.Observed.Trigger.Definition = "changed trigger definition"
		return matrixExpectation{source: source, action: ActionReplace, retain: true}
	case 23:
		refusal := droppedTargetRefusal("orders", "public.orders")
		source.Target = targetResolution{Outcome: targetDropped, Refusal: &refusal}
		return matrixExpectation{source: source, refusal: true}
	case 24:
		source.Target.Outcome = targetRenamed
		return matrixExpectation{source: source, action: ActionRename, retain: true}
	case 25:
		source.Target.Outcome = targetDroppedAndRecreated
		return matrixExpectation{source: source, action: ActionReplace, retain: true}
	}
	t.Fatalf("matrix criterion %d has no dedicated semantic fixture", criterion)
	return matrixExpectation{}
}

func ownedPairSource(t *testing.T, pair Pair) pairSources {
	t.Helper()
	marker := generatedMarker(t, "alpha", pair.Listener, pair.Operation)
	triggerDefinition := "trigger " + pair.Listener + "/" + pair.Operation
	functionDefinition := "function " + pair.Listener + "/" + pair.Operation
	return pairSources{
		Desired: desiredPair{Pair: pair, ListenerPresent: true, OperationPresent: true, Enabled: true, SpecHash: "same"},
		Recorded: recordedPair{Pair: pair, ListenerPresent: true, TriggerPresent: true,
			Listener: registryListener{Name: pair.Listener, SpecHash: "same", Enabled: true},
			Trigger: registryTrigger{Operation: pair.Operation, TriggerName: pair.Listener + "_" + pair.Operation,
				FunctionName: pair.Listener + "_fn_" + pair.Operation, DDLHash: fingerprint(triggerDefinition, functionDefinition)}},
		Observed: observedPair{Pair: pair, TriggerPresent: true, FunctionPresent: true,
			Trigger:  TriggerReading{Marker: &marker, Definition: triggerDefinition},
			Function: FunctionReading{Marker: &marker, Definition: functionDefinition}},
		Instance: "alpha",
	}
}

func assertClassification(t *testing.T, got pairClassification, want matrixExpectation) {
	t.Helper()
	if (got.Refusal != nil) != want.refusal {
		t.Fatalf("refusal = %v, want refusal %t", got.Refusal, want.refusal)
	}
	if got.Refusal != nil {
		if got.Action != nil {
			t.Fatalf("refusal also planned action %#v", got.Action)
		}
		return
	}
	if want.action == "" {
		if got.Action != nil {
			t.Fatalf("action = %#v, want clean", got.Action)
		}
		return
	}
	if got.Action == nil || got.Action.Kind != want.action || got.Action.Pair != want.source.pair() || got.RetainListener != want.retain {
		t.Fatalf("classification = %+v, want %s for pair %+v retained=%t", got, want.action, want.source.pair(), want.retain)
	}
}
