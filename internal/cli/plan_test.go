package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/reconcile"
)

func TestPlanRenderUsesLibraryAndPinsEverySymbol(t *testing.T) {
	result := reconcile.PlanResult{
		Actions: []reconcile.Action{
			{Pair: reconcile.Pair{Listener: "orders", Operation: "insert"}, Kind: reconcile.ActionCreate},
			{Pair: reconcile.Pair{Listener: "orders", Operation: "update"}, Kind: reconcile.ActionRename},
			{Pair: reconcile.Pair{Listener: "orders", Operation: "delete"}, Kind: reconcile.ActionReplace},
			{Pair: reconcile.Pair{Listener: "orders", Operation: "drop"}, Kind: reconcile.ActionDrop},
			{Pair: reconcile.Pair{Listener: "orders", Operation: "disable"}, Kind: reconcile.ActionDisable},
		},
	}
	want, err := os.ReadFile(filepath.Join("testdata", "plan.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Render(); got != string(want) {
		t.Fatalf("render = %q, golden = %q", got, string(want))
	}
	for _, tc := range []struct {
		kind   reconcile.ActionKind
		symbol string
	}{
		{reconcile.ActionCreate, "+"}, {reconcile.ActionRename, "~"},
		{reconcile.ActionReplace, "-/+"}, {reconcile.ActionDrop, "-"}, {reconcile.ActionDisable, "-"},
	} {
		if !strings.Contains(
			reconcile.PlanResult{Actions: []reconcile.Action{{Kind: tc.kind}}}.Render(),
			"    "+tc.symbol+" ",
		) {
			t.Fatalf("%s did not render symbol %q", tc.kind, tc.symbol)
		}
	}
}
