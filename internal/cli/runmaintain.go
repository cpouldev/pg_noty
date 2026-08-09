package cli

import (
	"context"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func runMaintenanceLoop(ctx context.Context, deps runDependencies) {
	interval := deps.maintenanceInterval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maintenancePass(ctx, deps)
		}
	}
}

func maintenancePass(ctx context.Context, deps runDependencies) {
	if deps.dryRun {
		_ = schema.PlanMaintenance(time.Now(), deps.cfg, nil)
		return
	}
	_, _ = schema.Maintain(ctx, deps.pool, deps.cfg, deps.schemaOpts)
	_, _ = schema.ApplyRetention(ctx, deps.pool, deps.cfg, deps.schemaOpts)
}
