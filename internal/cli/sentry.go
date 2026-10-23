package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/getsentry/sentry-go"
)

// sentryHandler keeps normal logging and reports error-level records as Sentry
// events. Its hub is bound at construction because worker log calls often have
// no context of their own.
type sentryHandler struct {
	local  slog.Handler
	report slog.Handler
}

func withSentry(logger *slog.Logger, hub *sentry.Hub) *slog.Logger {
	if hub == nil {
		return logger
	}
	return slog.New(&sentryHandler{
		local: logger.Handler(),
		// Use slog's encoding for both outputs so LogValuer redaction, groups,
		// and error formatting also apply to attributes sent to Sentry.
		report: slog.NewJSONHandler(sentryLogWriter{hub: hub}, &slog.HandlerOptions{Level: slog.LevelError}),
	})
}

func (handler *sentryHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.local.Enabled(ctx, level)
}

func (handler *sentryHandler) Handle(ctx context.Context, record slog.Record) error {
	localErr := handler.local.Handle(ctx, record.Clone())
	if handler.report.Enabled(ctx, record.Level) {
		return errors.Join(localErr, handler.report.Handle(ctx, record))
	}
	return localErr
}

func (handler *sentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sentryHandler{local: handler.local.WithAttrs(attrs), report: handler.report.WithAttrs(attrs)}
}

func (handler *sentryHandler) WithGroup(name string) slog.Handler {
	return &sentryHandler{local: handler.local.WithGroup(name), report: handler.report.WithGroup(name)}
}

type sentryLogWriter struct {
	hub *sentry.Hub
}

func (writer sentryLogWriter) Write(data []byte) (int, error) {
	var fields map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&fields); err != nil {
		return 0, err
	}
	if client := writer.hub.Client(); client != nil {
		message, _ := fields[slog.MessageKey].(string)
		event := client.EventFromMessage(message, sentry.LevelError)
		event.Logger = "pg_noty"
		event.Contexts["log"] = fields
		writer.hub.CaptureEvent(event)
	}
	return len(data), nil
}
