package cli

import (
	"github.com/cpouldev/pg_noty/internal/reconcile"
	"github.com/spf13/cobra"
)

func init() { registerCommand("plan", buildPlan) }

func buildPlan(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "show reconciliation changes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			loaded, pool, err := requireConfiguredPool(cmd, opts)
			if err != nil {
				return err
			}
			defer pool.Close()
			result, err := reconcile.Plan(commandContext(cmd), pool, *loaded.Value, reconcileOptions(opts.Logger))
			if err != nil {
				return err
			}
			_, _ = cmd.OutOrStdout().Write([]byte(result.Render()))
			return outcomeError{verdict: result.Verdict}
		},
	}
}
