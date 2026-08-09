package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

var documentedLogLevels = []string{"debug", "info", "warn", "error"}

func newLogger(levelName, format string, out io.Writer) (*slog.Logger, error) {
	level, err := parseLogLevel(levelName)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("log output is nil")
	}
	options := &slog.HandlerOptions{Level: level}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "text":
		return slog.New(slog.NewTextHandler(out, options)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(out, options)), nil
	default:
		return nil, fmt.Errorf("unrecognised log format %q", format)
	}
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unrecognised log level %q", value)
	}
}
