package schema

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
)

// This file is the only path in this package that can destroy data, and every line of it is written
// against that. It reads the world through catalog.go, decides through PlanMaintenance and nothing
// else, and issues every statement inside ddl.go's bounded transaction -- the same three
// authorities maintain.go uses, so a drop and a create cannot come to disagree about what exists
// (SC-7, SC-9).
//
// Four guards stand between a plan and a DROP, and each closes a different class of loss:
//
//	1. the ownership marker, read back *inside the drop's own transaction* -- a partition this
//	   instance cannot show it created is refused, whether it is unmarked, unreadable or another
//	   instance's, each with its own answer (retentionrefusal.go);
//	2. attachment to the configured event log, read from pg_inherits and never from the shape of a
//	   name -- a name match is forgeable and coincidental, and it is the predicate that deletes a
//	   customer's table while passing every ordinary input;
//	3. IF EXISTS, applied *after* both checks and never instead of either -- it suppresses the error
//	   from dropping a table that is not there and does nothing about dropping one that is;
//	4. plan and apply are separate -- Plan.Drop is PlanMaintenance's, a pure function, so a dry run
//	   is that call and issues no statement at all.
//
// It runs on maintain.go's pass value rather than a second one of its own. The two halves of a
// maintenance pass take the same three inputs and read the same world, and a second observation
// would be the drift SC-7 exists to prevent -- so observe, the plan and the counters are shared and
// only the verbs here are new.
//
// This file is the pass: what is observed, what is planned, and what each outcome is reported as.
// The one transaction that performs a drop -- and ADR-7's two mandatory comments, which belong where
// the DETACH is issued rather than where the pass begins -- is retentiondrop.go. The words are
// retentionreport.go and the reasons to refuse are retentionrefusal.go.

// ApplyRetention drops every partition whose whole extent has aged out of the retention window, and
// nothing else. on is the handle every statement runs on: the pool internal/cli holds, or
// bootstrap's single pinned connection -- both satisfy ddl.go's beginner.
//
// A refused range is not fatal to the ranges after it. Each is attempted, counted and logged on its
// own and the pass answers with every refusal together, because one range a down destination still
// holds must not stop the ranges behind it from being reclaimed.
//
// It does not store the DEFAULT partition's row gauge, although the observation reads it: that is
// ADR-8's standing condition about *creating* partitions, and Maintain is what stores it on every
// pass. Storing it here as well would move the same gauge from two passes at two cadences.
func ApplyRetention(ctx context.Context, on beginner, cfg config.Config, opts Options) (Result, error) {
	run := pass{on: on, cfg: cfg, opts: opts.normalized()}

	seen, err := run.observe(ctx)
	if err != nil {
		run.opts.Stats.CountFailure()
		run.opts.Logger.LogAttrs(
			ctx, levelOf(OutcomeFailed), retentionUnobservedMessage,
			slog.String(logCause, err.Error()),
		)
		return Result{Outcome: OutcomeFailed}, finished(err)
	}

	settled, refused := run.dropExpired(ctx, PlanMaintenance(seen.now, cfg, seen.partitions).Drop)
	run.opts.Logger.LogAttrs(
		ctx, levelOf(settled.Outcome), retentionSettledMessage,
		slog.String(logOutcome, string(settled.Outcome)), slog.Int(logDropped, settled.Dropped),
	)
	return settled, refused
}

// dropExpired attempts every range the plan asks for, in plan order, and answers with what the pass
// removed alongside every refusal it met.
//
// It takes the plan rather than computing one, which is guard 4's whole shape: the decision is a
// pure function of the instant, the configuration and the catalog, and this is the only code that
// issues anything. A caller wanting a dry run calls PlanMaintenance and stops.
func (run pass) dropExpired(ctx context.Context, expired []Range) (Result, error) {
	var settled Result
	var refused []error

	for _, ranged := range expired {
		switch err := run.dropOne(ctx, ranged); {
		case err == nil:
			settled.Dropped++
		case readsAsAlreadyGone(err) && run.noLongerCovered(ctx, ranged):
			run.opts.Logger.LogAttrs(
				ctx, slog.LevelInfo, retentionGoneMessage,
				slog.String(logRange, ranged.Name), slog.String(logExtent, extentOf(ranged)),
			)
		default:
			failure := dropRefused(ranged, err)
			run.countDropRefusal(err)
			run.opts.Logger.LogAttrs(
				ctx, levelOf(OutcomeFailed), retentionRefusedMessage,
				slog.String(logRange, ranged.Name), slog.String(logExtent, extentOf(ranged)),
				slog.String(logRemedy, remedyForDrop(err)), slog.String(logCause, failure.Error()),
			)
			refused = append(refused, failure)
		}
	}

	settled.Outcome = outcomeOf(settled.Dropped, len(refused))
	return settled, finished(errors.Join(refused...))
}

// dropOne removes one expired partition, in one bounded transaction, and reports the dead queue rows
// it reaped -- after the commit, because a reap that rolled back with its drop removed nothing.
func (run pass) dropOne(ctx context.Context, ranged Range) error {
	names, err := dropTargetsFor(run.cfg.Database.Schema, run.cfg.Instance, ranged)
	if err != nil {
		return err
	}

	var reaped int64
	refused := boundedTx(
		ctx, run.on, run.opts,
		ddlSubject{operation: droppingWork + ranged.Name, partition: ranged.Name},
		func(ctx context.Context, tx pgx.Tx) error {
			var err error
			reaped, err = run.dropWithinOneTransaction(ctx, tx, names, ranged)
			return err
		},
	)
	// boundedTx has already classified this error and passed it through the finishing point, in that
	// order (ddlrefusal.go); finishing it again is idempotent -- the text is the same and the
	// sentinel is found through Unwrap -- and it is what makes this declaration's compliance with
	// ADR-11 readable at the return rather than two calls away.
	if refused != nil {
		return finished(refused)
	}

	if reaped > 0 {
		run.opts.Logger.LogAttrs(
			ctx, slog.LevelInfo, retentionReapedMessage,
			slog.String(logRange, ranged.Name), slog.String(logExtent, extentOf(ranged)),
			slog.Int64(logReaped, reaped),
		)
	}
	return nil
}
