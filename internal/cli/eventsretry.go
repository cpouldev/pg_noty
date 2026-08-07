package cli

import (
	"fmt"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/spf13/cobra"
)

const retryBatchLimit = 1000

func buildEventsRetry(opts *rootOptions) *cobra.Command {
	var id int64
	var hasID bool
	var listener, status string
	command := &cobra.Command{
		Use: "retry", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			hasID = hasID || cmd.Flags().Changed("id")
			selector, err := retrySelector(id, hasID, listener, status)
			if err != nil {
				return err
			}
			loaded, pool, err := requireConfiguredPool(cmd, opts)
			if err != nil {
				return err
			}
			defer pool.Close()
			queueOptions := sourceOptions(opts.Logger)
			queue, err := source.Open(commandContext(cmd), pool, *loaded.Value, queueOptions)
			if err != nil {
				return err
			}
			defer queue.Close()
			if opts.DryRun {
				return nil
			}
			if selector.ID != nil {
				count, err := queue.RetryBatch(commandContext(cmd), selector, retryBatchLimit)
				if err != nil {
					return err
				}
				_, _ = cmd.OutOrStdout().Write([]byte(fmt.Sprintf("retried %d\n", count)))
				return nil
			}
			for {
				count, err := queue.RetryBatch(commandContext(cmd), selector, retryBatchLimit)
				if err != nil {
					return err
				}
				if count == 0 {
					return nil
				}
				_, _ = cmd.OutOrStdout().Write([]byte(fmt.Sprintf("retried %d\n", count)))
				select {
				case <-commandContext(cmd).Done():
					return commandContext(cmd).Err()
				default:
				}
			}
		},
	}
	command.Flags().Int64Var(&id, "id", 0, "event id")
	command.Flags().StringVar(&listener, "listener", "", "listener selector")
	command.Flags().StringVar(&status, "status", "", "queue status selector")
	return command
}

// retrySelector builds the selector and hands the rule to the package that enforces it, so the CLI
// cannot admit a selector RetryBatch then refuses with a different sentence.
func retrySelector(id int64, hasID bool, listener, status string) (source.QueueSelector, error) {
	selector := source.QueueSelector{Listener: listener, Status: status}
	if hasID {
		selector = source.QueueSelector{ID: &id, Listener: listener, Status: status}
	}
	if err := selector.ValidateForRetry(); err != nil {
		return source.QueueSelector{}, err
	}
	return selector, nil
}
