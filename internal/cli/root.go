package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type rootOptions struct {
	ConfigPath  string
	LogLevel    string
	LogFormat   string
	DryRun      bool
	AllowDelete bool
	AutoApprove bool
	Out         io.Writer
	Err         io.Writer
	In          io.Reader
	Logger      *slog.Logger
}

type commandBuilder func(*rootOptions) *cobra.Command

type registeredCommand struct {
	word  string
	build commandBuilder
}

var commandRegistry []registeredCommand

func registerCommand(word string, build commandBuilder) {
	if word == "" || build == nil {
		return
	}
	for _, existing := range commandRegistry {
		if existing.word == word {
			return
		}
	}
	commandRegistry = append(commandRegistry, registeredCommand{word: word, build: build})
}

func unregisterCommand(word string) {
	for index, command := range commandRegistry {
		if command.word != word {
			continue
		}
		commandRegistry = append(commandRegistry[:index], commandRegistry[index+1:]...)
		return
	}
}

func newRoot(opts *rootOptions) *cobra.Command {
	if opts == nil {
		opts = &rootOptions{}
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Err == nil {
		opts.Err = opts.Out
	}
	if opts.In == nil {
		opts.In = io.Reader(nil)
	}
	root := &cobra.Command{
		Use:   "pg_noty",
		Short: "PostgreSQL trigger reconciliation and delivery",
		// SilenceErrors keeps cobra from printing the error itself; the exit status still carries it.
		SilenceErrors: true,
		// Args is deliberately unset. cobra's default for a root that has subcommands is legacyArgs,
		// which refuses a first argument naming no subcommand; ArbitraryArgs disabled that check, and
		// because this root is not runnable the refusal became flag.ErrHelp, which ExecuteC swallows
		// into a nil error. `pg_noty plna -f listeners.yaml` then exited 0, the status reserved for
		// a clean plan. TestAnUnknownCommandIsRefusedRatherThanReportedClean holds both directions.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := newLogger(opts.LogLevel, opts.LogFormat, opts.Err)
			if err != nil {
				return err
			}
			opts.Logger = logger
			slog.SetDefault(logger)
			return nil
		},
	}
	root.SetOut(opts.Out)
	root.SetErr(opts.Err)
	root.SetIn(opts.In)
	flags := root.PersistentFlags()
	flags.StringVarP(&opts.ConfigPath, "config", "f", "", "configuration file")
	flags.StringVar(&opts.LogLevel, "log-level", "info", "log level: debug, info, warn or error")
	flags.StringVar(&opts.LogFormat, "log-format", "text", "log format: text or json")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "do not mutate the database")
	flags.BoolVar(&opts.AllowDelete, "allow-delete", false, "allow destructive changes")
	flags.BoolVar(&opts.AutoApprove, "auto-approve", false, "approve non-interactive changes")
	commands := slices.Clone(commandRegistry)
	slices.SortFunc(commands, func(left, right registeredCommand) int { return strings.Compare(left.word, right.word) })
	for _, registered := range commands {
		command := registered.build(opts)
		if command == nil {
			continue
		}
		root.AddCommand(command)
	}
	return root
}

func commandContext(cmd *cobra.Command) context.Context {
	if context := cmd.Context(); context != nil {
		return context
	}
	return context.Background()
}

func refuseCommand(cmd *cobra.Command) {
	cmd.Root().SilenceUsage = true
}

func requireLogger(opts *rootOptions) (*slog.Logger, error) {
	if opts == nil || opts.Logger == nil {
		return nil, fmt.Errorf("CLI logger is not initialized")
	}
	return opts.Logger, nil
}
