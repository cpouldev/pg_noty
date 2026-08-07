package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5"
)

// Ack removes the claimed queue row before recording exactly one delivery attempt. The append-only
// event log is deliberately absent from this transaction's writes.
func (s *TriggerSource) Ack(ctx context.Context, event Event, delivery Delivery) error {
	tx, err := s.beginTransition(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queue, err := qualifiedServiceTable(s.cfg.Database.Schema, schema.TableEventQueue)
	if err != nil {
		return err
	}
	query := "DELETE FROM " + queue + " WHERE event_id=$1 AND status=" + quoteLiteral("delivering") + " AND leased_by=$2 RETURNING event_id"
	if err := guardTransition(ctx, tx, query, event.ID, s.opts.LeasedBy); err != nil {
		return err
	}
	if err := recordDelivery(ctx, tx, s.cfg.Database.Schema, event, delivery); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ack: %w", err)
	}
	return nil
}

func (s *TriggerSource) beginTransition(ctx context.Context) (pgx.Tx, error) {
	if err := s.ensureUsable(); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transition: %w", err)
	}
	return tx, nil
}

func guardTransition(ctx context.Context, tx pgx.Tx, query string, args ...any) error {
	var id int64
	if err := tx.QueryRow(ctx, query, args...).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotClaimed
		}
		return fmt.Errorf("transition guard: %w", err)
	}
	return nil
}

func recordDelivery(ctx context.Context, tx pgx.Tx, serviceSchema string, event Event, delivery Delivery) error {
	deliveries, err := qualifiedServiceTable(serviceSchema, schema.TableDeliveries)
	if err != nil {
		return err
	}
	var status, snippet, recordedError any
	if delivery.HTTPStatus > 0 {
		status = delivery.HTTPStatus
	}
	if delivery.Snippet != "" {
		snippet = storableText(delivery.Snippet)
	}
	if delivery.Err != "" {
		recordedError = storableText(delivery.Err)
	}
	// RetryBatch resets attempts to 0, so a retried event re-walks an attempt number this table
	// already holds. A plain INSERT aborted the settle transaction there and the destination received
	// the event again; the conflict clause keeps the most recent outcome for an attempt number.
	query := "INSERT INTO " + deliveries + " (event_id, attempt, http_status, response_snippet, error, duration_ms, created_at) " +
		"VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp()) " +
		"ON CONFLICT (event_id, attempt) DO UPDATE SET http_status=EXCLUDED.http_status, " +
		"response_snippet=EXCLUDED.response_snippet, error=EXCLUDED.error, " +
		"duration_ms=EXCLUDED.duration_ms, created_at=EXCLUDED.created_at"
	_, err = tx.Exec(
		ctx,
		query,
		event.ID,
		event.Attempt,
		status,
		snippet,
		recordedError,
		delivery.Duration.Milliseconds(),
	)
	if err != nil {
		return fmt.Errorf("record delivery: %w", err)
	}
	return nil
}
