package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

const sentryShutdownTimeout = 2 * time.Second

var errInvalidSentryDSN = errors.New("SENTRY_DSN is invalid")

type sentryRuntime struct {
	client *sentry.Client
	hub    *sentry.Hub
}

func newSentryRuntime(dsn string, transport sentry.Transport) (*sentryRuntime, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, nil
	}
	if transport == nil {
		// The SDK's default telemetry processor flushes again during Close.
		// Use the asynchronous HTTP transport directly to keep shutdown bounded.
		transport = sentry.NewHTTPTransport()
	}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:              dsn,
		AttachStacktrace: true,
		DisableLogs:      true,
		DisableMetrics:   true,
		EnableTracing:    false,
		Transport:        transport,
	})
	if err != nil {
		return nil, errInvalidSentryDSN
	}
	scope := sentry.NewScope()
	scope.SetTag("component", "cli")
	return &sentryRuntime{client: client, hub: sentry.NewHub(client, scope)}, nil
}

func (runtime *sentryRuntime) context(ctx context.Context) context.Context {
	return sentry.SetHubOnContext(ctx, runtime.hub)
}

func (runtime *sentryRuntime) recoverPanic(ctx context.Context) {
	recovered := recover()
	if recovered == nil {
		return
	}
	runtime.hub.RecoverWithContext(ctx, recovered)
	// Reporting must not turn a process crash into apparent success.
	panic(recovered)
}

func (runtime *sentryRuntime) shutdown() {
	// A signal normally cancels the command context, so flushing needs an independent bound.
	ctx, cancel := context.WithTimeout(context.Background(), sentryShutdownTimeout)
	defer cancel()
	runtime.hub.FlushWithContext(ctx)
	runtime.client.Close()
}
