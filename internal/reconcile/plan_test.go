package reconcile

import (
	"slices"
	"testing"
)

func TestPlanActionsAreOrderedByListenerOperationThenKind(t *testing.T) {
	got := orderedActions([]Action{
		{Pair: Pair{Listener: "zebra", Operation: "update"}, Kind: ActionReplace},
		{Pair: Pair{Listener: "apple", Operation: "update"}, Kind: ActionRename},
		{Pair: Pair{Listener: "apple", Operation: "delete"}, Kind: ActionDrop},
		{Pair: Pair{Listener: "apple", Operation: "update"}, Kind: ActionCreate},
	})
	want := []Action{
		{Pair: Pair{Listener: "apple", Operation: "delete"}, Kind: ActionDrop},
		{Pair: Pair{Listener: "apple", Operation: "update"}, Kind: ActionCreate},
		{Pair: Pair{Listener: "apple", Operation: "update"}, Kind: ActionRename},
		{Pair: Pair{Listener: "zebra", Operation: "update"}, Kind: ActionReplace},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ordered actions = %#v, want %#v", got, want)
	}
}

func TestPlanDestructiveIsComputedFromEveryAction(t *testing.T) {
	for _, tc := range []struct {
		name string
		plan PlanResult
		want bool
	}{
		{"a drop", PlanResult{Actions: []Action{{Kind: ActionDrop}}}, true},
		{"only create replace and rename", PlanResult{Actions: []Action{
			{Kind: ActionCreate}, {Kind: ActionReplace}, {Kind: ActionRename},
		}}, false},
		{"an unclassified action", PlanResult{Actions: []Action{{Kind: ActionKind("future_action")}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.plan.Destructive(); got != tc.want {
				t.Fatalf("Destructive() = %t, want %t", got, tc.want)
			}
		})
	}
}
