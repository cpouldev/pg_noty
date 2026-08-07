package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() { registerCommand("validate", buildValidate) }

func buildValidate(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "validate a configuration without connecting to PostgreSQL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			if opts.ConfigPath == "" {
				return fmt.Errorf("configuration path is required")
			}
			loaded := loadConfig(opts.ConfigPath)
			output, err := renderLoadedDiagnostics(loaded, opts.LogFormat)
			if err != nil {
				return err
			}
			if output != "" {
				_, _ = cmd.OutOrStdout().Write([]byte(output))
			}
			if len(loaded.Errors) != 0 {
				return fmt.Errorf("configuration is invalid")
			}
			return nil
		},
	}
}
