package reconcile

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const renderSubprocessEnvironment = "PGNOTY_RENDER_IN_SUBPROCESS"

func renderedPlanFixture() PlanResult {
	return PlanResult{Actions: []Action{
		{Pair: Pair{Listener: "delta", Operation: "update"}, Kind: ActionRename},
		{Pair: Pair{Listener: "beta", Operation: "update"}, Kind: ActionReplace},
		{Pair: Pair{Listener: "alpha", Operation: "insert"}, Kind: ActionCreate},
		{Pair: Pair{Listener: "delta", Operation: "delete"}, Kind: ActionDisable},
		{Pair: Pair{Listener: "beta", Operation: "delete"}, Kind: ActionDrop},
	}}
}

// The reversed clone is supplied in genuinely different iteration order, then compared with the
// original input's bytes; repeated rendering alone would not expose an order inherited from a map.
func TestPlanRenderingIsByteIdenticalAcrossProcessesAndIterationOrders(t *testing.T) {
	plan := renderedPlanFixture()
	baseline := plan.Render()
	for range 1000 {
		if got := plan.Render(); got != baseline {
			t.Fatalf("in-process rendering differed:\n%s\nwant:\n%s", got, baseline)
		}
	}
	shuffled := slices.Clone(plan.Actions)
	slices.Reverse(shuffled) // The fixture supplied actions in the opposite iteration order.
	if got := (PlanResult{Actions: shuffled}).Render(); got != baseline {
		t.Fatalf("reversed input rendered:\n%s\nwant:\n%s", got, baseline)
	}
	if got := renderInSubprocess(t); got != baseline {
		t.Fatalf("subprocess rendering differed:\n%s\nwant:\n%s", got, baseline)
	}
}

// The expected bytes are derived from the fixture and the documented listener/operation/kind rule,
// not copied from a renderer run.
func TestPlanRenderingFollowsTheStatedTotalOrder(t *testing.T) {
	const want = "listener alpha:\n" +
		"  operation insert:\n" +
		"    + create\n" +
		"listener beta:\n" +
		"  operation delete:\n" +
		"    - drop\n" +
		"  operation update:\n" +
		"    -/+ replace\n" +
		"listener delta:\n" +
		"  operation delete:\n" +
		"    - disable\n" +
		"  operation update:\n" +
		"    ~ rename\n"
	if got := renderedPlanFixture().Render(); got != want {
		t.Fatalf("rendering =\n%s\nwant listener, then operation, then kind order:\n%s", got, want)
	}
}

// The renderer pins these specification literals independently of
// action_test.go.
func TestPlanRendererPinsTerraformSymbols(t *testing.T) {
	rendered := renderedPlanFixture().Render()
	for _, tc := range []struct{ literal, row string }{
		{"+", "+ create"},
		{"-/+", "-/+ replace"},
		{"-", "- drop"},
	} {
		if !strings.Contains(rendered, tc.row) {
			t.Errorf("rendering omits literal %q in row %q:\n%s", tc.literal, tc.row, rendered)
		}
	}
}

// This is deliberately per planned action, not a set-wide membership claim.
func TestEveryPlannedActionAppearsExactlyOnceInTheGolden(t *testing.T) {
	golden, err := readPlanGolden(allActionPlanGolden)
	if err != nil {
		t.Fatal(err)
	}
	rendered := renderedActions(t, golden)
	for _, action := range renderedPlanFixture().Actions {
		if got := countAction(rendered, action); got != 1 {
			t.Errorf("planned %#v appears %d times in the golden, want exactly once", action, got)
		}
	}
}

func TestEveryGoldenActionCorrespondsToAnActuallyPlannedAction(t *testing.T) {
	golden, err := readPlanGolden(allActionPlanGolden)
	if err != nil {
		t.Fatal(err)
	}
	planned := renderedPlanFixture().Actions
	for _, action := range renderedActions(t, golden) {
		if !slices.Contains(planned, action) {
			t.Errorf("golden action %#v has no planned action", action)
		}
	}
}

func TestGoldenActionCountEqualsThePlannedActionCount(t *testing.T) {
	golden, err := readPlanGolden(allActionPlanGolden)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(renderedActions(t, golden)), len(renderedPlanFixture().Actions); got != want {
		t.Fatalf("golden renders %d actions, want %d planned actions", got, want)
	}
}

func TestRenderInSubprocess(t *testing.T) {
	if os.Getenv(renderSubprocessEnvironment) == "" {
		return
	}
	fmt.Fprint(os.Stderr, renderedPlanFixture().Render())
}

func renderInSubprocess(t *testing.T) string {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestRenderInSubprocess$")
	command.Env = append(os.Environ(), renderSubprocessEnvironment+"=1")
	command.Stdout = io.Discard
	var rendered bytes.Buffer
	command.Stderr = &rendered
	if err := command.Run(); err != nil {
		t.Fatalf("subprocess render: %v", err)
	}
	return rendered.String()
}

func renderedActions(t *testing.T, rendering string) []Action {
	t.Helper()
	var actions []Action
	var listener, operation string
	for _, row := range strings.Split(strings.TrimSuffix(rendering, "\n"), "\n") {
		switch {
		case strings.HasPrefix(row, "listener "):
			listener = strings.TrimSuffix(strings.TrimPrefix(row, "listener "), ":")
		case strings.HasPrefix(row, "  operation "):
			operation = strings.TrimSuffix(strings.TrimPrefix(row, "  operation "), ":")
		case strings.HasPrefix(row, "    "):
			parts := strings.Fields(row)
			if len(parts) != 2 || listener == "" || operation == "" {
				t.Fatalf("malformed action row %q", row)
			}
			kind := ActionKind(parts[1])
			if parts[0] != kind.Symbol() {
				t.Fatalf("action row %q has symbol %q, want %q", row, parts[0], kind.Symbol())
			}
			actions = append(actions, Action{Pair: Pair{Listener: listener, Operation: operation}, Kind: kind})
		default:
			t.Fatalf("malformed rendered row %q", row)
		}
	}
	return actions
}

func countAction(actions []Action, want Action) int {
	count := 0
	for _, action := range actions {
		if action == want {
			count++
		}
	}
	return count
}
