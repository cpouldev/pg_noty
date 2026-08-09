package cli

import (
	"fmt"

	"github.com/cpouldev/pg_noty/internal/reconcile"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/spf13/cobra"
)

func init() { registerCommand("destroy", buildDestroy) }

func buildDestroy(opts *rootOptions) *cobra.Command {
	var confirmed bool
	command := &cobra.Command{
		Use: "destroy", Short: "remove objects owned by this instance", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			loaded, pool, err := requireConfiguredPool(cmd, opts)
			if err != nil {
				return err
			}
			defer pool.Close()
			queue, err := source.Open(commandContext(cmd), pool, *loaded.Value, sourceOptions(opts.Logger))
			if err != nil {
				return err
			}
			defer queue.Close()
			observation, err := queue.ObserveQueue(commandContext(cmd))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"queue pending=%d delivering=%d dead=%d\n",
				observation.Pending,
				observation.Delivering,
				observation.Dead,
			)
			if !confirmed {
				return fmt.Errorf("destroy requires --confirm")
			}
			if observation.Pending+observation.Delivering+observation.Dead != 0 && !opts.AllowDelete {
				return fmt.Errorf(
					"refusing destroy: queue has %d undelivered/dead events; pass --allow-delete",
					observation.Pending+observation.Delivering+observation.Dead,
				)
			}
			candidates, err := reconcile.OwnedCandidates(
				commandContext(cmd),
				pool,
				*loaded.Value,
				reconcileOptions(opts.Logger),
			)
			if err != nil {
				return err
			}
			for _, candidate := range candidates {
				ownership := reconcile.DetermineOwnership(candidate.Registry, candidate.Catalog, loaded.Value.Instance)
				if !ownership.Owned {
					_, _ = fmt.Fprintln(
						cmd.OutOrStdout(),
						destroyMessage(
							candidate.Catalog.Kind,
							ownership.Disagreement,
							candidate.Catalog.Identity,
							ownership.Found,
						),
					)
					return fmt.Errorf("ownership proof refused")
				}
			}
			if opts.DryRun {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "dry-run: %d owned candidates\n", len(candidates))
				return nil
			}
			destroyed := *loaded.Value
			destroyed.Listeners = nil
			result, err := reconcile.Apply(
				commandContext(cmd), pool, destroyed,
				reconcile.Approval{DestructionPermitted: true, Approved: true}, reconcileOptions(opts.Logger),
			)
			if err != nil {
				return err
			}
			if result.Verdict != reconcile.VerdictClean {
				return outcomeError{verdict: result.Verdict}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "destroyed %d owned objects\n", len(candidates))
			return nil
		},
	}
	command.Flags().BoolVar(&confirmed, "confirm", false, "confirm removal")
	return command
}
