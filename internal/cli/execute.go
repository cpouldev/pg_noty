package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/cpouldev/pg_noty/internal/reconcile"
)

// This file is a command's two ends: the entry point cmd/pg_noty calls, and the mapping from what a
// command returned to the process status. The mapping is reconcile.Verdict.ExitCode's and is quoted
// rather than re-declared, because internal/reconcile owns it -- CI branches on the 2.

func Execute(ctx context.Context, args []string, streams Streams) int {
	if ctx == nil {
		ctx = context.Background()
	}
	root := newRoot(&rootOptions{In: streams.In, Out: streams.Out, Err: streams.Err})
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	reportFailure(root.ErrOrStderr(), err)
	return exitStatusOf(err)
}

// reportFailure writes the message a failed command returned. SilenceErrors keeps cobra from
// printing usage beside a runtime failure, and it silenced the failure itself with it: every command
// that returned an error exited with a status and no explanation, so a refusal naming the command
// that answers it reached nobody.
//
// outcomeError is excluded because it carries a verdict rather than a fault. `plan` exiting 2 has
// already written its plan, and "command completed with reconciliation outcome" is the sentence that
// lets errors.As find the verdict, not one an operator needs to read.
func reportFailure(out io.Writer, err error) {
	var outcome outcomeError
	if err == nil || errors.As(err, &outcome) {
		return
	}
	_, _ = fmt.Fprintln(out, "error: "+err.Error())
}

type outcomeError struct{ verdict reconcile.Verdict }

func (e outcomeError) Error() string { return "command completed with reconciliation outcome" }

func exitStatusOf(err error) int {
	if err == nil {
		return reconcile.VerdictClean.ExitCode()
	}
	var outcome outcomeError
	if errors.As(err, &outcome) {
		return outcome.verdict.ExitCode()
	}
	return reconcile.VerdictError.ExitCode()
}
