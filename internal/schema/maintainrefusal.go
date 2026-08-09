package schema

import (
	"context"
	"errors"
	"log/slog"
)

// This file is what a maintenance pass does about a refusal: how it reports one that stopped the
// pass outright, and which counters one refused range moves. It is separate from maintain.go for the
// reason maintainreport.go was -- one file would breach the 200-line budget this package's own gate
// enforces -- and the seam is the one that changes for different reasons: maintain.go changes when
// the shape of a pass changes, this file when what a refusal costs an operator does.
//
// It reaches no driver and no catalog. Everything here reads an error and the counters, which is
// what lets criterion 33's observability be asserted container-free.

// reportFailure records a pass that stopped before it could plan anything: the failure counted so a
// caller with no metrics endpoint can read it (criterion 33), and the cause logged at the level that
// separates failure from routine operation.
//
// One helper rather than one block per reason, so two ways of giving up cannot come to be reported
// in two shapes. why is the line saying which question the pass could not answer, and it is a
// parameter because those reasons are distinct: a pass that could not read the catalog may succeed
// on the next tick, and one refusing the configuration it was handed will not.
//
// It records and returns nothing. What the pass then answers stays at the call site, finishing point
// and all -- both because a caller reads the control flow there rather than here, and because the
// gate behind ADR-11 reads the *call* to that finishing point in the function it is returned from
// (TestNoDriverReachingSourceReturnsAnErrorWithoutFinishingIt).
func (run pass) reportFailure(ctx context.Context, why string, err error) {
	run.opts.Stats.CountFailure()
	run.opts.Logger.LogAttrs(ctx, levelOf(OutcomeFailed), why, slog.String(logCause, err.Error()))
}

// countRefusal moves the counters one refused range calls for: the general failure count, which
// criterion 33 requires a caller to read with no metrics endpoint, and -- where the DEFAULT
// partition is what refused -- the distinct count ADR-8 gives that condition, because its remedy is
// a drain and not a retry.
func (run pass) countRefusal(err error) {
	run.opts.Stats.CountFailure()
	if errors.Is(err, ErrDefaultBlocked) {
		run.opts.Stats.CountDefaultPartitionBlocked()
	}
}
