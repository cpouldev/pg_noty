package reconcile

import (
	"context"
	"fmt"
	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"time"
)

type Options struct {
	LockTimeout, RunLockWait time.Duration
	Logger                   *slog.Logger
}
type Approval struct{ DestructionPermitted, Approved, Interactive bool }
type objectNames struct{ trigger, function string }

func Plan(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, opts Options) (PlanResult, error) {
	return planWith(ctx, pool, cfg, opts, planInTransaction)
}
func planWith(
	ctx context.Context,
	pool *pgxpool.Pool,
	cfg config.Config,
	opts Options,
	work func(context.Context, pgx.Tx, config.Config, *slog.Logger) (PlanResult, error),
) (PlanResult, error) {
	if pool == nil {
		return PlanResult{}, fmt.Errorf("plan has no pool")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PlanResult{}, fmt.Errorf("begin read-only plan: %w", err)
	}
	defer tx.Rollback(context.Background())
	return work(ctx, tx, cfg, optionsLogger(opts))
}
func optionsLogger(opts Options) *slog.Logger {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return opts.Logger
}
func traceWorkflow(logger *slog.Logger, step string) {
	logger.Debug("reconcile workflow", "step", step)
}
func planInTransaction(ctx context.Context, tx pgx.Tx, cfg config.Config, logger *slog.Logger) (PlanResult, error) {
	catalog := NewCatalog(tx)
	refusal, err := probeBootstrap(ctx, catalog, cfg.Database.Schema)
	if err != nil {
		return PlanResult{}, err
	}
	if refusal != nil {
		return PlanResult{Refusals: []Refusal{*refusal}, Verdict: VerdictError}, nil
	}
	diagnostics, err := validateInTransaction(ctx, tx, cfg)
	if err != nil {
		return PlanResult{}, err
	}
	if len(diagnostics) != 0 {
		return PlanResult{Diagnostics: diagnostics, Verdict: VerdictError}, nil
	}
	traceWorkflow(logger, "validation")
	return diffFromDatabase(ctx, tx, catalog, cfg, logger)
}
func validateInTransaction(ctx context.Context, tx pgx.Tx, cfg config.Config) (config.Errors, error) {
	checks := cfg.DeferredChecks()
	if err := validateDeferredKinds(checks); err != nil {
		return nil, err
	}
	validator := NewValidator(nil, cfg)
	catalog := NewCatalog(tx)
	var diagnostics config.Errors
	for _, check := range checks {
		diagnostic, reported, err := validator.validateCheck(ctx, tx, catalog, check)
		if err != nil {
			return nil, err
		}
		if reported {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	return diagnostics, nil
}
func diffFromDatabase(
	ctx context.Context,
	tx pgx.Tx,
	catalog Catalog,
	cfg config.Config,
	logger *slog.Logger,
) (PlanResult, error) {
	readings, err := readRegistries(ctx, tx, cfg.Database.Schema)
	if err != nil {
		return PlanResult{}, err
	}
	traceWorkflow(logger, "registry read")
	recordedByName := registryByName(readings)
	input := diffInput{Instance: cfg.Instance, Targets: make(map[string]targetResolution)}
	names := make(map[Pair]objectNames)
	for _, listener := range cfg.Listeners {
		if err := addConfiguredSource(
			ctx,
			catalog,
			cfg,
			listener,
			recordedByName[listener.Name],
			input.Targets,
			&input,
			names,
		); err != nil {
			return PlanResult{}, err
		}
	}
	for _, reading := range readings {
		if _, configured := input.Targets[reading.Listener.Name]; !configured {
			if err := addRecordedSource(ctx, catalog, reading, input.Targets); err != nil {
				return PlanResult{}, err
			}
		}
		for _, trigger := range reading.Triggers {
			pair := Pair{Listener: reading.Listener.Name, Operation: trigger.Operation}
			input.Recorded = append(
				input.Recorded,
				recordedPair{
					Pair: pair, ListenerPresent: true, TriggerPresent: true, Listener: reading.Listener,
					Trigger: trigger,
				},
			)
			names[pair] = objectNames{trigger: trigger.TriggerName, function: trigger.FunctionName}
		}
		if len(reading.Triggers) == 0 {
			input.Recorded = append(
				input.Recorded,
				recordedPair{
					Pair: Pair{Listener: reading.Listener.Name}, ListenerPresent: true, Listener: reading.Listener,
				},
			)
		}
	}
	for pair, names := range names {
		observed, err := readObservedPair(ctx, catalog, input.Targets[pair.Listener], pair, names)
		if err != nil {
			return PlanResult{}, err
		}
		input.Observed = append(input.Observed, observed)
	}
	traceWorkflow(logger, "ownership")
	return diff(input), nil
}
func addConfiguredSource(
	ctx context.Context,
	catalog Catalog,
	cfg config.Config,
	listener config.Listener,
	recorded registryReading,
	targets map[string]targetResolution,
	input *diffInput,
	names map[Pair]objectNames,
) error {
	var prior *registryListener
	if recorded.Listener.Name != "" {
		prior = &recorded.Listener
	}
	target, err := resolveListenerTarget(ctx, catalog, listener, prior)
	if err != nil {
		return err
	}
	targets[listener.Name] = target
	spec, err := deriveListenerSpec(listener.Trigger)
	if err != nil {
		return err
	}
	changes, known, err := listenerOperationChanges(recorded.Listener, listener.Trigger)
	if err != nil {
		return err
	}
	input.Listeners = append(
		input.Listeners,
		desiredListener{
			Name: listener.Name, SpecHash: spec.Hash, Enabled: listener.Enabled, SpecificationKnown: known,
			ChangedOperations: changes,
		},
	)
	if target.Refusal != nil {
		return nil
	}
	compiled, err := compileListener(cfg.Instance, cfg.Database.Schema, listener, target.Target)
	if err != nil {
		return err
	}
	for _, set := range compiled.Sets {
		pair := Pair{Listener: listener.Name, Operation: set.Operation}
		input.Desired = append(
			input.Desired,
			desiredPair{
				Pair: pair, ListenerPresent: true, OperationPresent: true, Enabled: listener.Enabled,
				SpecHash: spec.Hash,
			},
		)
		names[pair] = objectNames{trigger: set.TriggerName, function: set.FunctionName}
	}
	return nil
}
func addRecordedSource(
	ctx context.Context,
	catalog Catalog,
	reading registryReading,
	targets map[string]targetResolution,
) error {
	listener := config.Listener{
		Name: reading.Listener.Name, Trigger: config.TriggerSpec{Table: reading.Listener.TargetTable},
	}
	target, err := resolveListenerTarget(ctx, catalog, listener, &reading.Listener)
	if err == nil {
		targets[reading.Listener.Name] = target
	}
	return err
}
func readObservedPair(
	ctx context.Context,
	catalog Catalog,
	target targetResolution,
	pair Pair,
	names objectNames,
) (observedPair, error) {
	observed := observedPair{Pair: pair}
	if target.Refusal != nil {
		return observed, nil
	}
	trigger, found, err := catalog.ReadTrigger(ctx, target.Target.OID, names.trigger)
	if err != nil || !found {
		return observed, err
	}
	observed.Trigger, observed.TriggerPresent = trigger, true
	function, found, err := catalog.ReadFunction(ctx, trigger.FunctionOID)
	if err != nil || !found {
		return observed, err
	}
	observed.Function, observed.FunctionPresent = function, true
	return observed, nil
}
func syncRegistry(ctx context.Context, tx pgx.Tx, cfg config.Config, plan PlanResult, logger *slog.Logger) error {
	changed, configured := make(map[string]bool, len(plan.Actions)), configuredByName(cfg.Listeners)
	for _, action := range plan.Actions {
		changed[action.Pair.Listener] = true
	}
	for _, listener := range cfg.Listeners {
		if changed[listener.Name] {
			if err := syncConfiguredListener(ctx, tx, cfg, listener, logger); err != nil {
				return err
			}
		}
	}
	for _, action := range plan.Actions {
		if _, found := configured[action.Pair.Listener]; !found {
			traceWorkflow(logger, "registry write")
			if err := deleteRegistry(ctx, tx, cfg.Database.Schema, action.Pair.Listener); err != nil {
				return err
			}
		}
	}
	return nil
}
