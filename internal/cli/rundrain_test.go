package cli

import (
	"errors"
	"testing"

	"github.com/cpouldev/pg_noty/internal/delivery"
	"github.com/cpouldev/pg_noty/internal/reconcile"
)

func TestDrainVariantsUseTheSingleExitMapping(t *testing.T) {
	clean := delivery.DrainResult{Clean: true}
	if got := exitStatusOf(drainOutcome(clean)); got != reconcile.VerdictClean.ExitCode() {
		t.Fatalf("clean drain status = %d", got)
	}
	timed := delivery.DrainResult{TimedOut: true, Err: delivery.ErrDrainTimedOut}
	if !errors.Is(
		timed.Err,
		delivery.ErrDrainTimedOut,
	) || exitStatusOf(drainOutcome(timed)) == reconcile.VerdictClean.ExitCode() {
		t.Fatal("timed drain was reported clean")
	}
}

func drainOutcome(result delivery.DrainResult) error {
	if result.Success() {
		return nil
	}
	return outcomeError{verdict: reconcile.VerdictError}
}
