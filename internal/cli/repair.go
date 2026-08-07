package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

func init() { registerCommand("repair-default-partition", buildRepair) }

// RepairDefaultPartitionCommand is the sole CLI entry point to the operator-invoked drain.
// Keeping the call in this file gives the reach scan a symbol-derived target to guard.
func RepairDefaultPartitionCommand(
	ctx context.Context, pool *pgxpool.Pool, cfg config.Config,
	opts schema.Options, ranged schema.Range,
) (schema.RepairResult, error) {
	return schema.RepairDefaultPartition(ctx, pool, cfg, opts, ranged)
}

func buildRepair(opts *rootOptions) *cobra.Command {
	var fromText, toText, name string
	command := &cobra.Command{
		Use: "repair-default-partition", Short: "drain rows from DEFAULT", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			from, err := time.Parse(time.RFC3339Nano, fromText)
			if err != nil {
				return fmt.Errorf("invalid --from: %w", err)
			}
			to, err := time.Parse(time.RFC3339Nano, toText)
			if err != nil || !to.After(from) {
				return fmt.Errorf("invalid --to: %q", toText)
			}
			loaded, pool, err := requireConfiguredPool(cmd, opts)
			if err != nil {
				return err
			}
			defer pool.Close()
			if opts.DryRun {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would repair %s\n", name)
				return nil
			}
			libraryOptions := schemaOptions(opts.Logger)
			libraryOptions.Stats = &schema.MaintenanceStats{}
			result, err := RepairDefaultPartitionCommand(
				commandContext(cmd), pool, *loaded.Value, libraryOptions,
				schema.Range{From: from, To: to, Name: name},
			)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(), "repaired %s: moved=%d queue_moved=%d default_before=%d default_after=%d\n",
				result.Partition, result.Moved, result.QueueMoved, result.DefaultRowsBefore, result.DefaultRowsAfter,
			)
			return nil
		},
	}
	command.Flags().StringVar(&fromText, "from", "", "inclusive range start (RFC3339Nano)")
	command.Flags().StringVar(&toText, "to", "", "exclusive range end (RFC3339Nano)")
	command.Flags().StringVar(&name, "name", "", "partition name")
	return command
}
