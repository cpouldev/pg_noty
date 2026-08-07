package cli

import (
	"fmt"

	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/spf13/cobra"
)

func init() { registerCommand("status", buildStatus) }

func buildStatus(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use: "status", Short: "show schema, listener and queue status", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			loaded, pool, err := requireConfiguredPool(cmd, opts)
			if err != nil {
				return err
			}
			defer pool.Close()
			// The ledger is read before the queue. A database nothing has bootstrapped answers the queue
			// observation with `relation "noty.event_queue" does not exist (SQLSTATE 42P01)` -- precisely
			// the driver diagnostic internal/reconcile's bootstrap probe exists to keep away from an
			// operator who needs the remedy instead. It only became visible when a failed command started
			// reporting why it failed, and status is the command troubleshooting sends people to first.
			state, err := readSchemaState(commandContext(cmd), pool, *loaded.Value, schemaOptions(opts.Logger))
			if err != nil {
				return err
			}
			if !state.present {
				return fmt.Errorf(
					"schema %s has not been bootstrapped: run `pg_noty bootstrap`",
					loaded.Value.Database.Schema,
				)
			}
			libraryOptions := sourceOptions(opts.Logger)
			queue, err := source.Open(commandContext(cmd), pool, *loaded.Value, libraryOptions)
			if err != nil {
				return err
			}
			defer queue.Close()
			observation, err := queue.ObserveQueue(commandContext(cmd))
			if err != nil {
				return err
			}
			_, _ = cmd.OutOrStdout().Write(
				[]byte(fmt.Sprintf(
					"schema_version: %d\nlisteners: %d\nqueue_pending: %d\nqueue_delivering: %d\nqueue_dead: %d\nlag_seconds: %.3f\n",
					state.applied,
					len(loaded.Value.Listeners),
					observation.Pending,
					observation.Delivering,
					observation.Dead,
					observation.OldestPendingAge.Seconds(),
				)),
			)
			return nil
		},
	}
}

func statusError(err error) error { return fmt.Errorf("status: %w", err) }
