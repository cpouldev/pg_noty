package cli

import (
	"errors"
	"testing"

	"github.com/cpouldev/pg_noty/internal/reconcile"
)

func TestExitStatusQuotesVerdictMapping(t *testing.T) {
	for _, verdict := range []reconcile.Verdict{
		reconcile.VerdictClean, reconcile.VerdictChangesPending, reconcile.VerdictError,
	} {
		if got, want := exitStatusOf(outcomeError{verdict: verdict}), verdict.ExitCode(); got != want {
			t.Fatalf("%s exit status = %d, want %d", verdict, got, want)
		}
	}
	if got, want := exitStatusOf(nil), reconcile.VerdictClean.ExitCode(); got != want {
		t.Fatalf("nil exit status = %d, want %d", got, want)
	}
	if got, want := exitStatusOf(errors.New("failure")), reconcile.VerdictError.ExitCode(); got != want {
		t.Fatalf("error exit status = %d, want %d", got, want)
	}
}
