package cli

import (
	"io"
	"log/slog"
)

func slogTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
