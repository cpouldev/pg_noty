package cli

import (
	"context"

	"github.com/cpouldev/pg_noty/internal/delivery"
)

func deliveryResultHook(metrics *metrics) delivery.ResultHook {
	return func(_ context.Context, result delivery.DispatchResult) {
		outcome := result.Outcome.String()
		listener := result.Listener.Name
		if listener == "" {
			listener = "unknown"
		}
		metrics.deliveryAttempts.WithLabelValues(listener, outcome).Inc()
		metrics.deliveryDuration.WithLabelValues(
			listener,
			outcome,
		).Observe(result.Finished.Sub(result.Started).Seconds())
	}
}
