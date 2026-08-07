package cli

import "github.com/spf13/cobra"

func init() { registerCommand("events", buildEvents) }

func buildEvents(opts *rootOptions) *cobra.Command {
	parent := &cobra.Command{Use: "events", Short: "inspect and retry queued events"}
	parent.AddCommand(buildEventsList(opts), buildEventsRetry(opts))
	return parent
}
