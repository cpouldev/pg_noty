package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/delivery"
	"github.com/cpouldev/pg_noty/internal/reconcile"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

type runDependencies struct {
	cfg                 config.Config
	pool                *pgxpool.Pool
	latch               *reconcileLatch
	server              *runServer
	worker              *delivery.Worker
	metrics             *metrics
	schemaOpts          schema.Options
	logger              *slog.Logger
	maintenanceInterval time.Duration
	dryRun              bool
}

func init() { registerCommand("run", buildRun) }

func buildRun(opts *rootOptions) *cobra.Command {
	var chosen reconcileChoice
	var listenAddress string
	command := &cobra.Command{Use: "run", Short: "verify or reconcile, then serve deliveries", Args: cobra.NoArgs}
	command.Flags().BoolVar(
		&chosen.reconcile, "reconcile", false,
		"bootstrap and reconcile at startup, overriding auto_reconcile",
	)
	command.Flags().StringVar(&listenAddress, "listen", ":8080", "HTTP listen address")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		refuseCommand(cmd)
		// Settling queue rows is unconditional and bootstrapping is not, so the refusal names both.
		// No writing path reads --dry-run, so a run that honoured it would reconcile in full and
		// decline only the retention pass.
		if opts.DryRun {
			return fmt.Errorf(
				"run cannot be combined with --dry-run: it settles queue rows, and " +
					"bootstraps and reconciles when auto_reconcile is on; use `pg_noty plan` for a " +
					"read-only check",
			)
		}
		loaded, pool, err := requireConfiguredPool(cmd, opts)
		if err != nil {
			return err
		}
		defer pool.Close()
		chosen.reconcileSet = cmd.Flags().Changed("reconcile")
		mayMutate := chosen.decide(loaded.Value.AutoReconcile)
		stats := &schema.MaintenanceStats{}
		libraryOptions := schemaOptions(opts.Logger)
		libraryOptions.Stats = stats
		latch := &reconcileLatch{}
		metricSet := newMetrics(opts.Logger, stats)
		for _, listener := range loaded.Value.Listeners {
			metricSet.configureListeners([]string{listener.Name})
		}
		metricSet.partitionPrecreate = loaded.Value.Retention.Precreate
		deps := runDependencies{
			cfg: *loaded.Value, pool: pool, latch: latch, metrics: metricSet,
			schemaOpts: libraryOptions, logger: opts.Logger, maintenanceInterval: time.Minute, dryRun: opts.DryRun,
		}
		triggerSource, snapshot, sourceErr := openRunSource(
			commandContext(cmd),
			pool,
			*loaded.Value,
			opts.Logger,
			libraryOptions,
			metricSet,
		)
		if sourceErr != nil {
			return sourceErr
		}
		defer func() { _ = triggerSource.Close() }()
		ready := newReadiness(pool, *loaded.Value, latch, opts.Logger)
		running, err := bindRunServer(
			listenAddress,
			healthz(time.Now()),
			ready.handler(),
			runMetricsHandler(snapshot, metricSet.handler(opts.Logger)),
		)
		if err != nil {
			return err
		}
		deps.server = running
		go running.serve(opts.Logger)
		if err := startRun(commandContext(cmd), cmd, opts, &deps, running, mayMutate); err != nil {
			return err
		}
		allowed, err := delivery.ParseAllowedDestinations(loaded.Value.Worker.AllowedDestinationCIDRs)
		if err != nil {
			return err
		}
		deps.worker = delivery.NewWorker(triggerSource, triggerSource, workerConfig(*loaded.Value, metricSet, allowed))
		go runDeliveryWorker(commandContext(cmd), deps.worker, opts.Logger)
		go runMaintenanceLoop(commandContext(cmd), deps)
		<-commandContext(cmd).Done()
		return drainAndReport(commandContext(cmd), deps)
	}
	return command
}

// startRun discharges the startup obligation one of two ways. When run may change the database it
// bootstraps and applies, as it always did; when it may not, it verifies and refuses instead of
// serving blind. Either way the latch is resolved exactly once, so /readyz reports which happened.
func startRun(
	ctx context.Context, cmd *cobra.Command, opts *rootOptions, deps *runDependencies,
	running *runServer, mayMutate bool,
) error {
	if !mayMutate {
		return verifyRun(ctx, cmd, opts, deps, running)
	}
	if err := schema.Bootstrap(ctx, deps.pool, deps.cfg); err != nil {
		return refuseStartup(deps, running, err)
	}
	approval := approvalFor(opts, false)
	approval.Approved = true
	started := time.Now()
	result, err := reconcile.Apply(ctx, deps.pool, deps.cfg, approval, reconcileOptions(opts.Logger))
	deps.metrics.observeReconcile(started, result.Plan, err)
	if err != nil {
		return refuseStartup(deps, running, err)
	}
	if result.Verdict != reconcile.VerdictClean {
		reportRefusals(cmd.OutOrStdout(), result.Plan.Refusals)
		failure := fmt.Errorf("startup reconcile did not complete: %s", result.Verdict)
		deps.latch.resolve(failure)
		stopRunServer(running)
		return outcomeError{verdict: result.Verdict}
	}
	deps.latch.resolve(nil)
	return nil
}

func runMetricsHandler(snapshot *metricsSnapshot, next http.Handler) http.Handler {
	return http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			ctx, cancel := context.WithTimeout(request.Context(), 500*time.Millisecond)
			_ = snapshot.refresh(ctx)
			cancel()
			next.ServeHTTP(response, request)
		},
	)
}

func openRunSource(
	ctx context.Context,
	pool *pgxpool.Pool,
	cfg config.Config,
	logger *slog.Logger,
	options schema.Options,
	metricSet *metrics,
) (*source.TriggerSource, *metricsSnapshot, error) {
	sourceOpts := sourceOptions(logger)
	sourceOpts.LeasedBy, sourceOpts.Lease, sourceOpts.Listen = workerIdentity(cfg.Instance), cfg.Worker.LeaseTimeout, true
	triggerSource, err := source.Open(ctx, pool, cfg, sourceOpts)
	if err != nil {
		return nil, nil, err
	}
	snapshot := newMetricsSnapshot(
		time.Second, func(ctx context.Context) (source.QueueObservation, []schema.Range, error) {
			observation, err := triggerSource.ObserveQueue(ctx)
			if err != nil {
				return source.QueueObservation{}, nil, err
			}
			ranges, err := schema.ObservedPartitions(ctx, pool, cfg, options)
			return observation, ranges, err
		}, metricSet.applyQueueObservation,
	)
	return triggerSource, snapshot, nil
}
