package cli

import (
	"fmt"
	"strconv"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/reconcile"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

func init() { registerCommand("bootstrap", buildBootstrap) }

// buildBootstrap creates the service schema, applies the migrations this binary embeds, and
// pre-creates partitions. It is one of the two writing steps a deploy takes. It installs no
// trigger, because that is apply's half and it needs a different privilege -- TRIGGER on each target
// table, rather than CREATE on this database.
//
// It is also the command internal/reconcile has always named in its bootstrap refusal, which until
// now pointed at an action the CLI did not offer.
func buildBootstrap(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "create this instance's schema, tables and partitions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			loaded, pool, err := requireConfiguredPool(cmd, opts)
			if err != nil {
				return err
			}
			defer pool.Close()
			if opts.DryRun {
				return reportSchemaState(cmd, opts, pool, *loaded.Value)
			}
			if err := schema.Bootstrap(commandContext(cmd), pool, *loaded.Value); err != nil {
				return err
			}
			// Read back rather than reporting the version we meant to reach, so the line names what
			// the database records.
			state, err := readSchemaState(commandContext(cmd), pool, *loaded.Value, schemaOptions(opts.Logger))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(), "bootstrapped schema %s at version %d\n",
				loaded.Value.Database.Schema, state.applied,
			)
			return nil
		},
	}
}

// reportSchemaState is bootstrap's --dry-run. internal/schema has no statement-level dry run --
// PlanMaintenance covers partitions and nothing covers migrations -- so this reports the versions it
// can read rather than the DDL it would issue, and takes the changes-pending exit status when they
// disagree, so CI can branch on it the way it branches on plan.
func reportSchemaState(cmd *cobra.Command, opts *rootOptions, pool *pgxpool.Pool, cfg config.Config) error {
	state, err := readSchemaState(commandContext(cmd), pool, cfg, schemaOptions(opts.Logger))
	if err != nil {
		return err
	}
	applied := "absent"
	if state.present {
		applied = strconv.Itoa(state.applied)
	}
	_, _ = fmt.Fprintf(
		cmd.OutOrStdout(), "schema: %s\napplied_version: %s\nexpected_version: %d\n",
		cfg.Database.Schema, applied, state.expected,
	)
	if state.current() {
		return nil
	}
	return outcomeError{verdict: reconcile.VerdictChangesPending}
}
