package cli

import (
	"log/slog"

	"github.com/cpouldev/pg_noty/internal/reconcile"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
)

func schemaOptions(logger *slog.Logger) schema.Options { return schema.Options{Logger: logger} }

func reconcileOptions(logger *slog.Logger) reconcile.Options {
	return reconcile.Options{Logger: logger}
}

func sourceOptions(logger *slog.Logger) source.Options { return source.Options{Logger: logger} }

var optionConstructors = []func(*slog.Logger) any{
	func(logger *slog.Logger) any { return schemaOptions(logger) },
	func(logger *slog.Logger) any { return reconcileOptions(logger) },
	func(logger *slog.Logger) any { return sourceOptions(logger) },
}
