package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cpouldev/pg_noty/internal/cli"
	"github.com/getsentry/sentry-go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	status := run(
		ctx,
		os.Args[1:],
		cli.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr},
		os.Getenv("SENTRY_DSN"),
		nil,
	)
	stop()
	os.Exit(status)
}

func run(ctx context.Context, args []string, streams cli.Streams, dsn string, transport sentry.Transport) int {
	monitoring, err := newSentryRuntime(dsn, transport)
	if err != nil {
		_, _ = fmt.Fprintln(streams.Err, "error: "+err.Error())
		return 1
	}
	if monitoring == nil {
		return cli.Execute(ctx, args, streams)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = monitoring.context(ctx)
	// Defers unwind in reverse: capture a panic first, then flush it before os.Exit terminates.
	defer monitoring.shutdown()
	defer monitoring.recoverPanic(ctx)
	return cli.Execute(ctx, args, streams)
}
