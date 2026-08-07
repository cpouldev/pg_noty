package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestAnUnknownCommandIsRefusedRatherThanReportedClean pins the one exit status a mistyped command
// may not produce. Exit statuses 0 and 2 have meanings CI branches on -- clean, and changes
// pending -- so a command name the binary does not have must reach neither. The root carried
// Args: cobra.ArbitraryArgs, which disables cobra's unknown-command check; the root is not runnable,
// so execute() returned flag.ErrHelp, ExecuteC swallowed it, and `pg_noty plna -f listeners.yaml`
// exited 0 without opening a connection.
//
// Both directions are asserted. A repair that refuses every invocation would satisfy the first rows
// and fail the last two, which are the invocations that must still succeed.
func TestAnUnknownCommandIsRefusedRatherThanReportedClean(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		args     []string
		wantExit int
	}{
		{name: "a command the binary does not have", args: []string{"bogus-command"}, wantExit: 1},
		{name: "a near-miss for plan, whose 2 CI reads as changes pending",
			args: []string{"plna", "-f", "listeners.yaml"}, wantExit: 1},
		{name: "a near-miss for destroy", args: []string{"destory"}, wantExit: 1},
		{name: "a bare word before a real command", args: []string{"nope", "plan"}, wantExit: 1},
		// The two that must keep working: no arguments at all prints help, and an explicit help
		// request is not a failure.
		{name: "no arguments prints help", args: nil, wantExit: 0},
		{name: "an explicit help request", args: []string{"help"}, wantExit: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			got := Execute(context.Background(), testCase.args,
				Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if got != testCase.wantExit {
				t.Fatalf("Execute(%q) = %d, want %d; stdout=%q stderr=%q",
					testCase.args, got, testCase.wantExit, out.String(), errOut.String())
			}
		})
	}
}
