package source

import (
	"context"
	"fmt"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// Dead retains the queue row for retention while recording the final delivery attempt.
func (s *TriggerSource) Dead(ctx context.Context, event Event, delivery Delivery, reason string) error {
	tx, err := s.beginTransition(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queue, err := qualifiedServiceTable(s.cfg.Database.Schema, schema.TableEventQueue)
	if err != nil {
		return err
	}
	query := "UPDATE " + queue + " SET status=" + quoteLiteral("dead") + ", dead_reason=$3, leased_until=NULL, leased_by=NULL " +
		"WHERE event_id=$1 AND status=" + quoteLiteral("delivering") + " AND leased_by=$2 RETURNING event_id"
	if err := guardTransition(ctx, tx, query, event.ID, s.opts.LeasedBy, storableText(reason)); err != nil {
		return err
	}
	if err := recordDelivery(ctx, tx, s.cfg.Database.Schema, event, delivery); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dead: %w", err)
	}
	return nil
}
