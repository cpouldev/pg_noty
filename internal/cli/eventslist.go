package cli

import (
	"encoding/json"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/spf13/cobra"
)

func buildEventsList(opts *rootOptions) *cobra.Command {
	var listener, status string
	command := &cobra.Command{
		Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			selector := source.QueueSelector{Listener: listener, Status: status}
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
			events, err := queue.ListEvents(commandContext(cmd), selector)
			if err != nil {
				return err
			}
			data, err := json.Marshal(events)
			if err != nil {
				return err
			}
			_, _ = cmd.OutOrStdout().Write(append(data, '\n'))
			return nil
		},
	}
	command.Flags().StringVar(&listener, "listener", "", "listener selector")
	command.Flags().StringVar(&status, "status", "", "queue status selector")
	return command
}
