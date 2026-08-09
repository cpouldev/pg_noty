package cli

import (
	"context"
	"errors"
	"time"

	"github.com/cpouldev/pg_noty/internal/delivery"
	"github.com/cpouldev/pg_noty/internal/reconcile"
)

func drainAndReport(ctx context.Context, deps runDependencies) error {
	if deps.server != nil {
		shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := deps.server.shutdown(shutdownContext); err != nil {
			return err
		}
	}
	if deps.worker == nil {
		return nil
	}
	result := deps.worker.Drain(ctx, deps.cfg.Worker.DrainTimeout)
	if result.Success() {
		return nil
	}
	if errors.Is(result.Err, delivery.ErrDrainTimedOut) {
		if deps.logger != nil {
			deps.logger.Error("delivery drain timed out", "released", len(result.Released))
		}
		return outcomeError{verdict: reconcile.VerdictError}
	}
	return outcomeError{verdict: reconcile.VerdictError}
}
