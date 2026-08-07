package schema

import (
	"context"
	"log/slog"

	"github.com/cpouldev/pg_noty/internal/config"
)

// ObservedPartitions returns the bounded ranges currently attached to the event log.
// Observation remains owned by catalog.go; the DEFAULT partition is intentionally not a range.
func ObservedPartitions(ctx context.Context, on catalogReader, cfg config.Config, opts Options) ([]Range, error) {
	opts = opts.normalized()
	found, err := observePartitions(ctx, on, cfg.Database.Schema, TableEvents)
	if err != nil {
		return nil, err
	}
	opts.Logger.LogAttrs(
		ctx, slog.LevelDebug, "observed event partitions",
		slog.Int("bounded", len(found.Bounded)), slog.Bool("default", found.Default != ""),
	)
	return found.Bounded, nil
}
