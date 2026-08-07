package reconcile

import (
	"slices"
	"testing"
)

// Criteria 15-18 are named for properties of a whole listener, and a single pairSources cannot hold
// any of them. "One operation removed" and "every operation removed" are byte-identical as one pair
// -- OperationPresent false -- and so are "this listener removed" and "every listener removed". A
// matrix that fixtures them that way makes the two inputs the criterion exists to separate answer
// alike because they are the same input.
//
// These four rows therefore vary a listener: how many of its pairs the configuration still declares,
// and whether it declares the listener at all.

// pairOutcome is what a scope must produce for one of its pairs. An empty action means the pair is
// left alone; retained is whether the listener's own registry row survives the action.
type pairOutcome struct {
	pair     Pair
	action   ActionKind
	retained bool
}

// listenerScope is one matrix row whose subject is a listener rather than a pair. outcomes are
// written in the plan's own total order, so each planned action is bound to the pair it belongs to
// rather than to a set of acceptable actions.
type listenerScope struct {
	input       diffInput
	outcomes    []pairOutcome
	destructive bool
}

// managedListener is one listener as all three sources see it: every operation is recorded and
// observed, configured names the subset the configuration still declares, and present is whether the
// configuration declares the listener at all.
type managedListener struct {
	name                   string
	operations, configured []string
	present                bool
}

func scopeInput(t *testing.T, listeners ...managedListener) diffInput {
	t.Helper()
	input := diffInput{Instance: "alpha"}
	for _, listener := range listeners {
		for _, operation := range listener.operations {
			source := ownedPairSource(t, Pair{Listener: listener.name, Operation: operation})
			input.Recorded = append(input.Recorded, source.Recorded)
			input.Observed = append(input.Observed, source.Observed)
			if slices.Contains(listener.configured, operation) {
				input.Desired = append(input.Desired, source.Desired)
			}
		}
		if listener.present {
			input.Listeners = append(input.Listeners, desiredListener{Name: listener.name, SpecHash: "same", Enabled: true})
		}
	}
	return input
}

// removedOperationScope is criterion 15: the listener keeps insert and loses update, so one of its
// pairs survives the run and its registry row plainly has to.
func removedOperationScope(t *testing.T) listenerScope {
	return listenerScope{
		input: scopeInput(t, managedListener{name: "orders", operations: []string{"insert", "update"},
			configured: []string{"insert"}, present: true}),
		outcomes: []pairOutcome{
			{pair: Pair{Listener: "orders", Operation: "insert"}},
			{pair: Pair{Listener: "orders", Operation: "update"}, action: ActionDrop, retained: true},
		},
		destructive: true,
	}
}

// noOperationsRetainedScope is criterion 16: the listener is still configured and declares no
// operation at all, so every pair drops and the registry row survives anyway. This is the row a
// classifier keyed on "no triggers remain" answers wrongly, and the row above is the one it answers
// correctly, which is why they must not share a fixture.
func noOperationsRetainedScope(t *testing.T) listenerScope {
	return listenerScope{
		input: scopeInput(t, managedListener{name: "orders", operations: []string{"insert", "update"}, present: true}),
		outcomes: []pairOutcome{
			{pair: Pair{Listener: "orders", Operation: "insert"}, action: ActionDrop, retained: true},
			{pair: Pair{Listener: "orders", Operation: "update"}, action: ActionDrop, retained: true},
		},
		destructive: true,
	}
}

// listenerRemovedScope is criterion 17: one registry listener is absent from an otherwise managed
// configuration, so its pairs drop with its row while the configured listener is left alone.
func listenerRemovedScope(t *testing.T) listenerScope {
	return listenerScope{
		input: scopeInput(t,
			managedListener{name: "orders", operations: []string{"insert"}, configured: []string{"insert"}, present: true},
			managedListener{name: "retired", operations: []string{"delete"}}),
		outcomes: []pairOutcome{
			{pair: Pair{Listener: "orders", Operation: "insert"}},
			{pair: Pair{Listener: "retired", Operation: "delete"}, action: ActionDrop},
		},
		destructive: true,
	}
}

// emptyListenerListScope is criterion 18's first half: an explicitly empty listeners list is a
// configuration, and it removes everything this instance owns rather than one listener's share of it.
// Its other half -- that an absent listeners key is not a configuration at all -- is below.
func emptyListenerListScope(t *testing.T) listenerScope {
	return listenerScope{
		input: scopeInput(t,
			managedListener{name: "orders", operations: []string{"insert", "update"}},
			managedListener{name: "retired", operations: []string{"delete"}}),
		outcomes: []pairOutcome{
			{pair: Pair{Listener: "orders", Operation: "insert"}, action: ActionDrop},
			{pair: Pair{Listener: "orders", Operation: "update"}, action: ActionDrop},
			{pair: Pair{Listener: "retired", Operation: "delete"}, action: ActionDrop},
		},
		destructive: true,
	}
}

func assertListenerScope(t *testing.T, scope listenerScope) {
	t.Helper()
	assertScopePlan(t, diff(scope.input), scope)
	sources := collectPairSources(scope.input)
	for _, outcome := range scope.outcomes {
		assertPairOutcome(t, classifyPair(preparedPairSource(scope.input, sources, outcome.pair)), outcome)
	}
}

func assertScopePlan(t *testing.T, result PlanResult, scope listenerScope) {
	t.Helper()
	planned := plannedOutcomes(scope)
	if result.Examined != len(scope.outcomes) || len(result.Actions) != len(planned) || len(result.Refusals) != 0 {
		t.Fatalf("scope plan = %+v, want %d examined pairs and %d actions", result, len(scope.outcomes), len(planned))
	}
	for index, want := range planned {
		assertActionKind(t, result.Actions[index], want.pair, want.action)
	}
	if result.Destructive() != scope.destructive {
		t.Fatalf("scope plan Destructive() = %t, want %t", result.Destructive(), scope.destructive)
	}
}

func assertPairOutcome(t *testing.T, got pairClassification, want pairOutcome) {
	t.Helper()
	if want.action == "" {
		if got.Refusal != nil || got.Action != nil {
			t.Fatalf("pair %+v classified %+v, want this scope to leave it alone", want.pair, got)
		}
		return
	}
	assertClassifiedAction(t, got, want.pair, want.action)
	if got.RetainListener != want.retained {
		t.Fatalf("pair %+v planned %s with listener retention %t, want %t", want.pair, want.action, got.RetainListener, want.retained)
	}
}

func plannedOutcomes(scope listenerScope) []pairOutcome {
	var planned []pairOutcome
	for _, outcome := range scope.outcomes {
		if outcome.action != "" {
			planned = append(planned, outcome)
		}
	}
	return planned
}
