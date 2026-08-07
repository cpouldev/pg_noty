package schema

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
)

// This file is one maintenance pass: read the world, decide, execute, count, log. It owns no
// observation and no execution of its own -- the world is read through catalog.go, the decision is
// PlanMaintenance's, every statement runs inside ddl.go's bounded transaction -- because a second
// observer is how a create and a drop come to disagree about what exists (SC-7) and a second
// execution path is one that escapes the availability bound (SC-9, M5). maintainreport.go and
// maintainrefusal.go are the other halves: the words a pass reports in and the statement it writes,
// and what it does about a refusal.
//
// Criterion 22's idempotency is structural rather than guarded: a converged catalog produces an
// empty Plan.Create, and an empty plan holds no statement to issue. Nothing here tests a flag
// before executing, so there is nothing to delete and leave the claim resting on air.
//
// ADR-8, Accepted, is as much about what this pass does *not* do. It moves no row: no DELETE and no
// INSERT against the DEFAULT partition under any input, the blocked-range case included. It detects
// that state, counts it under its own name and logs it pointing at RepairDefaultPartition -- Step
// 15's operator-invoked drain, written in a message and never called, so a scan asserting that
// prohibition reads calls and not text. Retention is absent for the same reason: this pass executes
// Plan.Create alone, so Result.Dropped is zero on every path here, and Plan.Drop is Step 14's.

// Maintain converges the observed partition set on the plan for now, and answers what the pass
// amounted to. on is the handle every statement runs on: the pool internal/cli holds, or the single
// pinned connection bootstrap keeps for its whole run -- both satisfy ddl.go's beginner, so neither
// caller has to unwrap the other's handle.
//
// A refused range is not fatal to the ranges after it. Each is attempted, counted and logged on its
// own and the pass answers with every refusal together, because one blocked or contended day must
// not leave the days behind it uncovered -- and an uncovered day is a customer write landing in
// DEFAULT.
func Maintain(ctx context.Context, on beginner, cfg config.Config, opts Options) (Result, error) {
	run := pass{on: on, cfg: cfg, opts: opts.normalized()}

	// Before the world is read, because no reading changes the answer and the set an unservable
	// configuration asks for is what PlanMaintenance would allocate below (plan.go).
	if unservable := unservableHorizonIn(cfg.Retention); unservable != nil {
		run.reportFailure(ctx, unservableMessage, unservable)
		return Result{Outcome: OutcomeFailed}, finished(unservable)
	}

	seen, err := run.observe(ctx)
	if err != nil {
		run.reportFailure(ctx, unobservedMessage, err)
		return Result{Outcome: OutcomeFailed}, finished(err)
	}

	// ADR-8's standing condition, stored on every pass rather than only where a create was refused:
	// by the time DefaultPartitionBlocked moves, the cheap window to drain has usually closed.
	// Stored and never added, because rows sitting in DEFAULT are a state and not an event.
	run.opts.Stats.StoreDefaultPartitionRows(seen.defaultRows)

	settled, refused := run.converge(ctx, PlanMaintenance(seen.now, cfg, seen.partitions).Create)
	run.opts.Logger.LogAttrs(
		ctx, levelOf(settled.Outcome), settledMessage,
		slog.String(logOutcome, string(settled.Outcome)), slog.Int(logCreated, settled.Created),
		slog.Int64(logDefaultRows, seen.defaultRows),
	)
	return settled, refused
}

// pass is one run's inputs, gathered so the steps below read as a story rather than as four
// parameters threaded through each. opts is normalised by Maintain and by nothing after it.
type pass struct {
	on   beginner
	cfg  config.Config
	opts Options
}

// observation is the world one pass decided against: read inside a single transaction, so now(),
// the partitions and the DEFAULT partition's row count are one reading rather than three instants.
type observation struct {
	now         time.Time
	partitions  []Range
	defaultRows int64
}

// observe reads that world through catalog.go, bounded like every other statement this package
// issues: the count against the DEFAULT partition takes a lock a conflicting holder can keep
// indefinitely (M5, criterion 40).
//
// A parent carrying no DEFAULT partition is refused rather than read as one holding no rows --
// catalog.go answers an empty name for it and defaultPartitionRows refuses that name -- because a
// schema whose write-availability net is gone is not one to create partitions in. That refusal is
// reached by TestAnEventLogWithNoDefaultPartitionIsRefusedRatherThanMaintained.
func (run pass) observe(ctx context.Context) (observation, error) {
	var seen observation

	err := boundedTx(
		ctx, run.on, run.opts, ddlSubject{operation: observingWork},
		func(ctx context.Context, tx pgx.Tx) error {
			var err error
			if seen.now, err = dbNow(ctx, tx); err != nil {
				return err
			}
			found, err := observePartitions(ctx, tx, run.cfg.Database.Schema, TableEvents)
			if err != nil {
				return err
			}
			seen.partitions = found.Bounded
			seen.defaultRows, err = defaultPartitionRows(ctx, tx, run.cfg.Database.Schema, found.Default)
			return err
		},
	)
	if err != nil {
		return observation{}, finished(fmt.Errorf("%s: %w", observingWork, err))
	}
	return seen, nil
}

// converge attempts every range the plan asks for, in plan order, and answers with what the pass
// changed alongside every refusal it met.
func (run pass) converge(ctx context.Context, wanted []Range) (Result, error) {
	var settled Result
	var refused []error

	for _, ranged := range wanted {
		switch err := run.createOne(ctx, ranged); {
		case err == nil:
			settled.Created++
		case run.covers(ctx, ranged):
			run.opts.Logger.LogAttrs(
				ctx, slog.LevelInfo, concededMessage,
				slog.String(logRange, ranged.Name), slog.String(logExtent, extentOf(ranged)),
			)
		default:
			failure := creationRefused(ranged, err)
			run.countRefusal(err)
			run.opts.Logger.LogAttrs(
				ctx, levelOf(OutcomeFailed), refusedMessage,
				slog.String(logRange, ranged.Name), slog.String(logExtent, extentOf(ranged)),
				slog.String(logRemedy, remedyFor(err)), slog.String(logCause, failure.Error()),
			)
			refused = append(refused, failure)
		}
	}

	settled.Outcome = outcomeOf(settled.Created, len(refused))
	return settled, finished(errors.Join(refused...))
}

// createOne creates one partition and marks it as ours, in one bounded transaction.
//
// The marker is written inside the create's own transaction rather than after it, because an
// unmarked partition is one Step 14's drop guard refuses to drop for as long as it exists, and a
// marker written by a second statement is one a crash between the two leaves off (ADR-3). The
// rendered partition name comes from the marked object rather than from a second call to the
// quoting authority, so the create and the marker cannot address different objects.
func (run pass) createOne(ctx context.Context, ranged Range) error {
	schemaName := run.cfg.Database.Schema

	marked, fault := partitionObject(schemaName, ranged.Name, run.cfg.Instance)
	if fault != IdentifierOK {
		return finished(
			fmt.Errorf(
				"the partition of schema %s for %s cannot be created: the name "+
					"%s given for it %s", schemaName, extentOf(ranged), ranged.Name, fault,
			),
		)
	}
	parent, fault := Qualified(schemaName, TableEvents)
	if fault != IdentifierOK {
		return finished(
			fmt.Errorf(
				"the event log of schema %s cannot be named: %s %s",
				schemaName, TableEvents, fault,
			),
		)
	}

	return boundedTx(
		ctx, run.on, run.opts,
		ddlSubject{operation: creatingWork + ranged.Name, partition: ranged.Name},
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, createPartition(marked.target, parent, ranged)); err != nil {
				return err
			}
			return claimMarker(ctx, tx, marked)
		},
	)
}

// covers reports whether a partition covering exactly this range exists now, which is how a lost
// creation race is reconciled: by the *observed bound*, never by the name the statement wrote (M6).
// The membership question is plan.go's own, so a race cannot be reconciled under a rule the plan
// does not use -- a partition a DBA created covers the extent under a name this process would never
// choose, and asking for it again is what the server answers with `would overlap partition ...`.
//
// An observation that itself failed answers false, so the create's own refusal is what the caller
// is told about: this reconciliation may discharge an error, never introduce one.
func (run pass) covers(ctx context.Context, ranged Range) bool {
	var observed []Range

	err := boundedTx(
		ctx, run.on, run.opts, ddlSubject{operation: observingWork},
		func(ctx context.Context, tx pgx.Tx) error {
			found, err := observePartitions(ctx, tx, run.cfg.Database.Schema, TableEvents)
			observed = found.Bounded
			return err
		},
	)
	return err == nil && len(rangesNotYetObserved([]Range{ranged}, observed)) == 0
}
