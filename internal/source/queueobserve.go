package source

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
)

var _ QueueAdmin = (*TriggerSource)(nil)

func (s *TriggerSource) ObserveQueue(ctx context.Context) (QueueObservation, error) {
	if err := s.ensureUsable(); err != nil {
		return QueueObservation{}, err
	}
	queue, err := qualifiedServiceTable(s.cfg.Database.Schema, schema.TableEventQueue)
	if err != nil {
		return QueueObservation{}, err
	}
	pending, delivering, dead := quoteLiteral("pending"), quoteLiteral("delivering"), quoteLiteral("dead")
	query := `SELECT COALESCE(SUM(depth) FILTER (WHERE status=` + pending + `),0),
COALESCE(SUM(depth) FILTER (WHERE status=` + delivering + `),0),
COALESCE(SUM(depth) FILTER (WHERE status=` + dead + `),0),
COALESCE(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(oldest) FILTER (WHERE status=` + pending + `))),0),
COALESCE(jsonb_object_agg(listener, depth) FILTER (WHERE status=` + dead + `),jsonb_build_object())
FROM (SELECT listener, status, count(*) AS depth, min(occurred_at) AS oldest
      FROM ` + queue + ` GROUP BY listener, status) AS grouped`
	var observation QueueObservation
	var age float64
	var deadJSON []byte
	if err := s.pool.QueryRow(ctx, query).Scan(
		&observation.Pending, &observation.Delivering,
		&observation.Dead, &age, &deadJSON,
	); err != nil {
		return QueueObservation{}, fmt.Errorf("observe queue: %w", err)
	}
	observation.OldestPendingAge = time.Duration(age * float64(time.Second))
	if err := json.Unmarshal(deadJSON, &observation.DeadByListener); err != nil {
		return QueueObservation{}, fmt.Errorf("decode dead queue counts: %w", err)
	}
	return observation, nil
}
