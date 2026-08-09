package cli

import (
	"time"

	"github.com/cpouldev/pg_noty/internal/reconcile"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
)

func (m *metrics) applyQueueObservation(observation source.QueueObservation, ranges []schema.Range) {
	m.queueDepth.WithLabelValues("pending").Set(float64(observation.Pending))
	m.queueDepth.WithLabelValues("delivering").Set(float64(observation.Delivering))
	m.queueDepth.WithLabelValues("dead").Set(float64(observation.Dead))
	m.oldestPending.Set(observation.OldestPendingAge.Seconds())
	m.partitionCoverage.Set(schema.CoverageShortfall(ranges, time.Now(), m.partitionPrecreate).Seconds())
	dead := observation.DeadByListener
	if len(m.listeners) != 0 {
		filtered := make(map[string]int64, len(m.listeners))
		for listener, count := range dead {
			if _, allowed := m.listeners[listener]; allowed {
				filtered[listener] = count
			}
		}
		dead = filtered
	}
	m.deadEvents.Replace(dead)
}

func (m *metrics) observeReconcile(start time.Time, result reconcile.PlanResult, err error) {
	m.reconcileDuration.Observe(time.Since(start).Seconds())
	if err != nil || result.Verdict == reconcile.VerdictError {
		m.reconcileErrors.Inc()
	}
}
