package cli

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"os"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/delivery"
)

// This file is how the CLI builds, identifies and supervises the delivery worker: the translation
// from a validated configuration into delivery's vocabulary, the identity a claimed row records, and
// the restart loop around Worker.Run.

// workerConfig is the translation from a validated configuration to the delivery engine's own
// vocabulary. allowed carries worker.allowed_destination_cidrs, already parsed by the caller so a
// malformed entry refuses the command rather than reaching a dialer.
func workerConfig(cfg config.Config, metrics *metrics, allowed []*net.IPNet) delivery.WorkerConfig {
	listeners := make([]delivery.ListenerConfig, 0, len(cfg.Listeners))
	for _, listener := range cfg.Listeners {
		if !listener.Enabled {
			continue
		}
		listeners = append(
			listeners, delivery.ListenerConfig{
				Name: listener.Name, URL: listener.Delivery.Destination.URL,
				Method:  listener.Delivery.Destination.Method,
				Headers: cloneHeaders(listener.Delivery.Destination.Headers),
				Signer:  delivery.NewSigner(listener.Delivery.Destination.Signing.Secrets),
				Policy: delivery.Policy{
					MaxAttempts:     listener.Delivery.Retry.MaxAttempts,
					Backoff:         listener.Delivery.Retry.Backoff,
					InitialInterval: listener.Delivery.Retry.InitialInterval,
					MaxInterval:     listener.Delivery.Retry.MaxInterval,
					Jitter:          listener.Delivery.Retry.Jitter,
				},
				Timeout: listener.Delivery.Timeout, Concurrency: listener.Delivery.Concurrency,
				MaxPayloadBytes: listener.Trigger.Payload.MaxBytes, AllowedDestinations: allowed,
			},
		)
	}
	return delivery.WorkerConfig{
		BatchSize: cfg.Worker.BatchSize, GlobalConcurrency: cfg.Worker.Concurrency,
		PollInterval: cfg.Worker.PollInterval, LeaseTimeout: cfg.Worker.LeaseTimeout,
		DrainTimeout: cfg.Worker.DrainTimeout, Listeners: listeners, OnResult: deliveryResultHook(metrics),
	}
}

// cloneHeaders copies the configured headers so the worker cannot mutate the loaded configuration.
func cloneHeaders(headers config.Headers) map[string]string {
	return maps.Clone(headers)
}

// workerIdentity is what a claimed queue row records in leased_by, and it must differ between any
// two processes that could hold a lease at the same time.
//
// config.Instance alone will not do, which is why host and pid join it: instance defaults to the
// service schema name, so every replica of a deployment would write the same string and the
// ownership guard in Ack, Nack and Dead would compare it against itself.
// custody_integration_test.go proves that guard works only because it opens its two sources as
// "worker-a" and "worker-b"; hand both the same word and a worker whose lease had already been
// reclaimed could still settle the row its successor was delivering -- an Ack deleting a live row,
// or worse a Nack returning it to pending while a request was still in flight.
//
// The instance stays first because an operator reading noty.event_queue is usually asking which
// deployment holds a row; host and pid answer which process. A pid is reused over time, but only
// after the process holding it has gone, and a lease outlives its holder by at most its own
// timeout.
func workerIdentity(instance string) string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s/%s/%d", instance, host, os.Getpid())
}

// runDeliveryWorker supervises the wake loop rather than merely reporting its death.
//
// Worker.Run returns on the first poll error, and a poll error is ordinarily transient -- a
// connection reset, a failover, a pool acquire that timed out. Logging it and returning left the
// process alive, serving /readyz 200 from the pool, the schema version and the startup reconcile,
// none of which observes the worker, while nothing was delivered until someone restarted the pod.
// The poll interval is the safety net behind the notification fast path, so retrying is what it is
// for. TestTheDeliverySupervisorRestartsAfterATransientFailure holds it.
func runDeliveryWorker(ctx context.Context, worker *delivery.Worker, logger *slog.Logger) {
	for attempt := 0; ctx.Err() == nil; attempt++ {
		err := worker.Run(ctx)
		if ctx.Err() != nil || err == nil {
			return
		}
		if logger != nil {
			logger.Error("delivery worker stopped; restarting", "error", err, "attempt", attempt+1)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(deliveryRestartBackoff(attempt)):
		}
	}
}

// deliveryRestartBackoff keeps a destination-independent failure -- the database being unreachable --
// from becoming a hot loop, while staying short enough that a brief failover costs one interval.
func deliveryRestartBackoff(attempt int) time.Duration {
	const base, ceiling = time.Second, 30 * time.Second
	backoff := base << min(attempt, 5)
	return min(backoff, ceiling)
}
