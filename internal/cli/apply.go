package cli

import (
	"fmt"
	"time"

	"github.com/cpouldev/pg_noty/internal/reconcile"
	"github.com/spf13/cobra"
)

func init() { registerCommand("apply", buildApply) }

func buildApply(opts *rootOptions) *cobra.Command {
	var runLockWait time.Duration
	command := &cobra.Command{
		Use:   "apply",
		Short: "apply reconciliation changes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			loaded, pool, err := requireConfiguredPool(cmd, opts)
			if err != nil {
				return err
			}
			defer pool.Close()
			if opts.DryRun {
				result, planErr := reconcile.Plan(
					commandContext(cmd),
					pool,
					*loaded.Value,
					reconcileOptions(opts.Logger),
				)
				if planErr != nil {
					return planErr
				}
				_, _ = cmd.OutOrStdout().Write([]byte(result.Render()))
				return outcomeError{verdict: result.Verdict}
			}
			approval := approvalFor(opts, stdoutIsInteractive())
			if approval.Interactive && !approval.Approved {
				// The plan has to be read before the prompt, or the prompt is asked about nothing.
				// This gate fired on every interactive apply, so a converged configuration printed
				// "Apply destructive changes?" and a bare Enter -- the documented default -- exited 1
				// for a run that had no changes to make. Plan takes no lock and Apply re-diffs rather
				// than trusting an earlier plan (TestApplyReDiffsInsteadOfTrustingAnEarlierPlan), so
				// reading it here costs one round trip and creates no window to act on stale state.
				pending, planErr := reconcile.Plan(
					commandContext(cmd),
					pool,
					*loaded.Value,
					reconcileOptions(opts.Logger),
				)
				if planErr != nil {
					return planErr
				}
				if len(pending.Actions) != 0 && !confirm(commandContext(cmd), cmd.InOrStdin(), cmd.OutOrStdout()) {
					return fmt.Errorf("changes were not confirmed")
				}
			}
			libraryOptions := reconcileOptions(opts.Logger)
			libraryOptions.RunLockWait = runLockWait
			result, applyErr := reconcile.Apply(commandContext(cmd), pool, *loaded.Value, approval, libraryOptions)
			if applyErr != nil {
				return applyErr
			}
			reportRefusals(cmd.OutOrStdout(), result.Plan.Refusals)
			return outcomeError{verdict: result.Verdict}
		},
	}
	command.Flags().DurationVar(&runLockWait, "run-lock-wait", 0, "maximum wait for the reconcile run lock")
	return command
}
