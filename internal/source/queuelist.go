package source

import (
	"context"
	"fmt"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func (s *TriggerSource) ListEvents(ctx context.Context, selector QueueSelector) ([]QueueEvent, error) {
	if err := s.ensureUsable(); err != nil {
		return nil, err
	}
	where, args, err := selectorPredicateQualified(selector, 1, false, "q.")
	if err != nil {
		return nil, err
	}
	queue, err := qualifiedServiceTable(s.cfg.Database.Schema, schema.TableEventQueue)
	if err != nil {
		return nil, err
	}
	events, err := qualifiedServiceTable(s.cfg.Database.Schema, schema.TableEvents)
	if err != nil {
		return nil, err
	}
	query := `SELECT e.id, e.occurred_at, e.listener, e.operation, e.table_name, e.payload,
q.status, q.attempts, q.next_attempt_at, q.dead_reason FROM ` + queue + ` q
JOIN ` + events + ` e ON e.id=q.event_id AND e.occurred_at=q.occurred_at`
	if where != "" {
		query += " WHERE" + where
	}
	query += " ORDER BY e.id"
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	var found []QueueEvent
	for rows.Next() {
		var event QueueEvent
		if err := rows.Scan(
			&event.ID, &event.OccurredAt, &event.Listener, &event.Operation, &event.Table,
			&event.Payload, &event.Status, &event.Attempts, &event.NextAttemptAt, &event.DeadReason,
		); err != nil {
			return nil, fmt.Errorf("scan listed event: %w", err)
		}
		found = append(found, event)
	}
	return found, rows.Err()
}
