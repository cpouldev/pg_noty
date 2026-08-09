package cli

import (
	"fmt"
	"os"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

type loadedConfig struct {
	Value    *config.Config
	Source   []byte
	Warnings config.Warnings
	Errors   config.Errors
}

func loadConfig(path string) loadedConfig {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return loadedConfig{Errors: config.Errors{config.NewFileError(config.RuleRead, path, readErr.Error())}}
	}
	value, warnings, errs := config.Parse(data, path, os.LookupEnv)
	return loadedConfig{Value: value, Source: data, Warnings: warnings, Errors: errs}
}

// requireConfig is the precondition every configuration-reading command shares: load it, render any
// diagnostics in the chosen format, and refuse rather than continue. Ten command builders each
// carried a byte-identical copy, and two had already drifted onto a second diagnostics path.
func requireConfig(cmd *cobra.Command, opts *rootOptions) (loadedConfig, error) {
	loaded := loadConfig(opts.ConfigPath)
	if len(loaded.Errors) != 0 {
		if output, renderErr := renderLoadedDiagnostics(loaded, opts.LogFormat); renderErr == nil {
			_, _ = cmd.OutOrStdout().Write([]byte(output))
		}
		return loaded, fmt.Errorf("configuration is invalid")
	}
	if loaded.Value == nil {
		return loaded, fmt.Errorf("configuration is empty")
	}
	return loaded, nil
}

// requireConfiguredPool adds the connection the database-touching commands open. The pool is
// returned rather than closed here so the caller keeps its own defer.
func requireConfiguredPool(cmd *cobra.Command, opts *rootOptions) (loadedConfig, *pgxpool.Pool, error) {
	loaded, err := requireConfig(cmd, opts)
	if err != nil {
		return loaded, nil, err
	}
	pool, err := schema.OpenPool(commandContext(cmd), *loaded.Value)
	if err != nil {
		return loaded, nil, err
	}
	return loaded, pool, nil
}
