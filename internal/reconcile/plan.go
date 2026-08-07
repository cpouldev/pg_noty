package reconcile

import (
	"cmp"
	"slices"

	"github.com/cpouldev/pg_noty/internal/config"
)

// Pair is one listener and operation examined by reconciliation.
type Pair struct {
	Listener  string
	Operation string
}

// Action is one planned change for a pair.
type Action struct {
	Pair Pair
	Kind ActionKind
}

// PlanResult is the value returned by planning. Actions are read in the stated total order:
// listener, then operation, then kind. Examined counts pairs examined, not actions produced, so a
// plan that found no changes remains distinguishable from one that examined nothing. A replace is
// rendered -/+ because PostgreSQL has no in-place trigger-definition alteration; rendering it ~
// would tell a Terraform-literate operator that the object never stops existing.
type PlanResult struct {
	Actions     []Action
	Examined    int
	Refusals    []Refusal
	Diagnostics config.Errors
	Verdict     Verdict
}

// Destructive reports whether any planned action requires destruction permission.
func (result PlanResult) Destructive() bool {
	for _, action := range result.Actions {
		if action.Kind.Destructive() {
			return true
		}
	}
	return false
}

// orderedActions returns a sorted copy, so rendering cannot depend on collection iteration order.
func orderedActions(actions []Action) []Action {
	ordered := slices.Clone(actions)
	slices.SortFunc(ordered, compareActions)
	return ordered
}

func compareActions(left, right Action) int {
	if byListener := cmp.Compare(left.Pair.Listener, right.Pair.Listener); byListener != 0 {
		return byListener
	}
	if byOperation := cmp.Compare(left.Pair.Operation, right.Pair.Operation); byOperation != 0 {
		return byOperation
	}
	return cmp.Compare(left.Kind, right.Kind)
}
