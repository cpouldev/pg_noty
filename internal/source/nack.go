package source

import (
	"context"
	"fmt"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// Nack supplies a delay from Go while PostgreSQL supplies the current epoch, avoiding absolute-time
// clock skew between the worker and the database.
func (s *TriggerSource) Nack(ctx context.Context, event Event, delivery Delivery, retryAt time.Time) error {
	tx, err := s.beginTransition(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queue, err := qualifiedServiceTable(s.cfg.Database.Schema, schema.TableEventQueue)
	if err != nil {
		return err
	}
	delay := retryDelay(s, retryAt).Seconds()
	query := "UPDATE " + queue + " SET status=" + quoteLiteral("pending") + ", next_attempt_at=now()+make_interval(secs => $3), " +
		"leased_until=NULL, leased_by=NULL WHERE event_id=$1 AND status=" + quoteLiteral("delivering") + " AND leased_by=$2 RETURNING event_id"
	if err := guardTransition(ctx, tx, query, event.ID, s.opts.LeasedBy, delay); err != nil {
		return err
	}
	if err := recordDelivery(ctx, tx, s.cfg.Database.Schema, event, delivery); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit nack: %w", err)
	}
	return nil
}

func retryDelay(source *TriggerSource, retryAt time.Time) time.Duration {
	delay := retryAt.Sub(source.now())
	if delay < 0 {
		return 0
	}
	return delay
}
