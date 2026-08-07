package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/spf13/cobra"
)

func init() { registerCommand("grants", buildGrants) }

func buildGrants(opts *rootOptions) *cobra.Command {
	var role string
	command := &cobra.Command{
		Use:   "grants",
		Short: "emit the minimal PostgreSQL privilege statements",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			if opts.ConfigPath == "" {
				return fmt.Errorf("configuration path is required")
			}
			if reservedRoleName(role) {
				return fmt.Errorf("refusing reserved role %q", role)
			}
			loaded := loadConfig(opts.ConfigPath)
			output, err := renderLoadedDiagnostics(loaded, opts.LogFormat)
			if err != nil {
				return err
			}
			if len(loaded.Errors) != 0 {
				_, _ = cmd.OutOrStdout().Write([]byte(output))
				return fmt.Errorf("configuration is invalid")
			}
			if loaded.Value == nil {
				return fmt.Errorf("configuration is empty")
			}
			targets := configuredTargets(loaded.Value)
			statements := schema.GrantStatements(loaded.Value.Database.Schema, role, targets)
			if len(statements) == 0 {
				return fmt.Errorf("configuration has no usable grant set")
			}
			_, _ = cmd.OutOrStdout().Write([]byte(strings.Join(statements, "\n") + "\n"))
			return nil
		},
	}
	command.Flags().StringVar(&role, "role", "noty", "PostgreSQL role to grant")
	return command
}

func reservedRoleName(role string) bool { return strings.HasPrefix(role, "pg_") }

func configuredTargets(value *config.Config) []string {
	targets := make([]string, 0, len(value.Listeners))
	for _, listener := range value.Listeners {
		targets = append(targets, listener.Trigger.Table)
	}
	slices.Sort(targets)
	return slices.Compact(targets)
}
