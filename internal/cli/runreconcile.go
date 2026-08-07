package cli

import (
	"context"
	"fmt"

	"github.com/cpouldev/pg_noty/internal/reconcile"
	"github.com/spf13/cobra"
)

// reconcileChoice is the --reconcile flag plus whether it was written, which is what separates "the
// operator asked for this" from "this is the flag's zero value". Without the Changed half, a default
// of false is indistinguishable from --reconcile=false and the configuration key could never win.
type reconcileChoice struct {
	reconcile    bool
	reconcileSet bool
}

// decide resolves whether this run may change the database. The flag beats the file, so a one-off
// can be forced without editing a mounted configuration; the file beats the built-in false, so a
// deployment declares the policy once.
func (choice reconcileChoice) decide(configured bool) bool {
	if choice.reconcileSet {
		return choice.reconcile
	}
	return configured
}

// verifyRun is what run does when it may not change the database. It reads the schema version and
// plans -- Plan holds a read-only transaction and rolls it back, so neither writes -- and refuses to
// serve unless the database already matches the configuration.
//
// It refuses rather than serving because the alternative is the failure this default would otherwise
// ship: a worker polling a queue no trigger fills, delivering nothing, with every health check
// green. A silent skip was defensible while it took a flag to ask for it; as the default it is not.
func verifyRun(
	ctx context.Context,
	cmd *cobra.Command,
	opts *rootOptions,
	deps *runDependencies,
	running *runServer,
) error {
	state, err := readSchemaState(ctx, deps.pool, deps.cfg, schemaOptions(opts.Logger))
	if err != nil {
		return refuseStartup(deps, running, err)
	}
	// The schema is checked before the plan, so a database with no schema at all is told to
	// bootstrap rather than shown the reconciler's refusal about the registry tables it could not
	// read. Both are true of that database, so the order is behaviour rather than description:
	// TestRunWithoutAutoReconcileCreatesNothingOnADatabaseNothingHasBootstrapped violates both at
	// once and asserts which of the two answers.
	if !state.current() {
		return refuseStartup(deps, running, staleSchema(deps.cfg.Database.Schema, state))
	}

	result, err := reconcile.Plan(ctx, deps.pool, deps.cfg, reconcileOptions(opts.Logger))
	if err != nil {
		return refuseStartup(deps, running, err)
	}
	if result.Verdict != reconcile.VerdictClean {
		_, _ = cmd.OutOrStdout().Write([]byte(result.Render()))
		reportRefusals(cmd.OutOrStdout(), result.Refusals)
		deps.latch.resolve(fmt.Errorf("the database does not match the configuration: %s", result.Verdict))
		stopRunServer(running)
		return outcomeError{verdict: result.Verdict}
	}
	deps.latch.resolve(nil)
	return nil
}

// staleSchema names which of the three states the ledger is in and the command that answers it. A
// binary older than its database is its own reason with its own remedy: bootstrapping it would only
// reach a version the database has already passed, so it is told to roll forward instead.
func staleSchema(serviceSchema string, state schemaState) error {
	switch {
	case !state.present:
		return fmt.Errorf(
			"schema %s has not been bootstrapped and auto_reconcile is off: run "+
				"`pg_noty bootstrap` and `pg_noty apply`, or set auto_reconcile: true", serviceSchema,
		)
	case state.applied > state.expected:
		return fmt.Errorf(
			"schema %s records version %d and this binary embeds only version %d, so "+
				"the database is ahead of the binary: roll the binary forward rather than bootstrapping",
			serviceSchema, state.applied, state.expected,
		)
	default:
		return fmt.Errorf(
			"schema %s records version %d and this binary embeds version %d, and "+
				"auto_reconcile is off: run `pg_noty bootstrap`, or set auto_reconcile: true",
			serviceSchema, state.applied, state.expected,
		)
	}
}

// refuseStartup resolves the readiness latch with the failure and stops the server bound before it,
// so a path that stops serving cannot forget to say on /readyz why it did.
func refuseStartup(deps *runDependencies, running *runServer, failure error) error {
	deps.latch.resolve(failure)
	stopRunServer(running)
	return failure
}
