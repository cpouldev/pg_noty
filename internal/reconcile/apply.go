package reconcile

import (
	"context"
	"errors"
	"fmt"
	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"strings"
)

type ApplyResult struct {
	Plan       PlanResult
	Statements int
	Verdict    Verdict
	Stopped    StopReason
}

var errApplyStopped = errors.New("apply stopped")

func Apply(
	ctx context.Context,
	pool *pgxpool.Pool,
	cfg config.Config,
	approval Approval,
	opts Options,
) (result ApplyResult, returned error) {
	if pool == nil {
		return result, fmt.Errorf("apply has no pool")
	}
	held, refusal, err := acquireRunLock(ctx, pool, cfg.Database.Schema, cfg.Instance, opts.RunLockWait)
	if err != nil {
		return result, err
	}
	if refusal != nil {
		return ApplyResult{
			Plan: PlanResult{Refusals: []Refusal{*refusal}, Verdict: VerdictError}, Verdict: VerdictError,
			Stopped: StopRunLockWaitExpired,
		}, nil
	}
	defer func() {
		releaseCtx := ctx
		if ctx.Err() != nil {
			releaseCtx = context.WithoutCancel(ctx)
		}
		if err := held.release(releaseCtx); returned == nil && err != nil {
			returned = err
		}
	}()
	logger := optionsLogger(opts)
	result.Statements, err = applyInTransactionWithTimeout(
		ctx, held.on, pool, opts.LockTimeout, nil, func(run *applyTx) error {
			plan, err := planInTransaction(ctx, run.tx, cfg, logger)
			if err != nil {
				return err
			}
			result.Plan = plan
			if len(plan.Diagnostics) != 0 {
				result.Verdict, result.Stopped = VerdictError, StopValidation
				return errApplyStopped
			}
			if len(plan.Refusals) != 0 {
				stop := StopOwnership
				if strings.Contains(plan.Refusals[0].Message(), "targets missing table") {
					stop = StopTargetDropped
				}
				result.Verdict, result.Stopped = VerdictError, stop
				return errApplyStopped
			}
			if len(plan.Actions) == 0 {
				return nil
			}
			traceWorkflow(logger, "decision")
			if plan.Destructive() && !approval.DestructionPermitted {
				if err := reportLiveQueueCounts(ctx, run.tx, cfg, plan, logger); err != nil {
					return err
				}
			}
			decision := Decide(
				plan.Destructive(),
				approval.DestructionPermitted,
				approval.Approved,
				approval.Interactive,
			)
			if !decision.Proceed {
				result.Verdict, result.Stopped = VerdictChangesPending, decision.Stopped
				return errApplyStopped
			}
			objects, err := objectsForPlan(ctx, run.tx, cfg, plan)
			if err != nil {
				return err
			}
			if err := run.apply(ctx, objects); err != nil {
				return err
			}
			return syncRegistry(ctx, run.tx, cfg, plan, logger)
		},
	)
	if err == errApplyStopped {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Verdict = VerdictClean
	logger.Debug("reconcile applied", "statements", result.Statements)
	return result, nil
}

// reportLiveQueueCounts is the one refusal-time queue observation; internal/delivery owns every
// other read.
func reportLiveQueueCounts(
	ctx context.Context,
	tx pgx.Tx,
	cfg config.Config,
	plan PlanResult,
	logger *slog.Logger,
) error {
	configured, reported := configuredByName(cfg.Listeners), map[string]bool{}
	for _, action := range plan.Actions {
		if action.Kind != ActionDrop || configured[action.Pair.Listener].Name != "" || reported[action.Pair.Listener] {
			continue
		}
		counts, err := readQueueCounts(ctx, tx, cfg.Database.Schema, action.Pair.Listener)
		if err != nil {
			return err
		}
		logger.Debug(
			"listener removal queue",
			"listener",
			action.Pair.Listener,
			"live",
			counts.Live,
			"dead",
			counts.Dead,
		)
		reported[action.Pair.Listener] = true
	}
	return nil
}
func syncConfiguredListener(
	ctx context.Context,
	tx pgx.Tx,
	cfg config.Config,
	listener config.Listener,
	logger *slog.Logger,
) error {
	reading, err := readRegistry(ctx, tx, cfg.Database.Schema, listener.Name)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var prior *registryListener
	if reading.Listener.Name != "" {
		prior = &reading.Listener
	}
	resolution, err := resolveListenerTarget(ctx, NewCatalog(tx), listener, prior)
	if err != nil {
		return err
	}
	spec, err := deriveListenerSpec(listener.Trigger)
	if err != nil {
		return err
	}
	target, fault := schema.Qualified(resolution.Target.Schema, resolution.Target.Table)
	if fault != schema.IdentifierOK {
		return fmt.Errorf("catalog target is unusable: %s", fault)
	}
	traceWorkflow(logger, "readback")
	triggers, err := registryTriggers(ctx, tx, cfg, listener, resolution.Target)
	if err != nil {
		return err
	}
	traceWorkflow(logger, "registry write")
	_, err = writeRegistry(
		ctx,
		tx,
		cfg.Database.Schema,
		registryListener{
			Name: listener.Name, Spec: spec.Bytes, SpecHash: spec.Hash, TargetTable: target,
			TargetOID: resolution.Target.OID, Enabled: listener.Enabled,
		},
		triggers,
	)
	return err
}
func registryTriggers(
	ctx context.Context,
	tx pgx.Tx,
	cfg config.Config,
	listener config.Listener,
	target TargetReading,
) ([]registryTrigger, error) {
	if !listener.Enabled {
		return nil, nil
	}
	compiled, err := compileListener(cfg.Instance, cfg.Database.Schema, listener, target)
	if err != nil {
		return nil, err
	}
	triggers := make([]registryTrigger, 0, len(compiled.Sets))
	for _, set := range compiled.Sets {
		reading, found, err := NewCatalog(tx).ReadPair(ctx, target.OID, set.TriggerName)
		if err != nil || !found {
			return nil, err
		}
		triggers = append(
			triggers,
			registryTrigger{
				Operation: set.Operation, TriggerName: set.TriggerName, FunctionName: set.FunctionName,
				DDLHash: fingerprint(reading.Trigger.Definition, reading.Function.Definition),
			},
		)
	}
	return triggers, nil
}
