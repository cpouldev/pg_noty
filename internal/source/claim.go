package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5"
)

// claimQueryFor renders internal/delivery's committed query for one service schema.
//
// database.schema is the author's to choose and merely defaults to noty, so a query that named noty
// outright read a different schema than Ack, Nack, Dead and readClaimedEvents -- silently returning
// nothing while the configured queue filled. Every service table here is therefore rendered through
// internal/schema's quoting authority, which is the same authority those four use.
//
// Exactly one transformation separates this text from internal/schema/testdata/claim_query.sql,
// and it is the quoting: the fixture writes the default schema bare, as the delivery worker's
// schematic does. TestClaimQueryMatchesTheCommittedFixture asserts that by undoing it, so a drift
// in either text fails there and internal/schema's committed plan keeps describing the query that
// runs.
func claimQueryFor(serviceSchema string) (string, error) {
	queue, err := qualifiedServiceTable(serviceSchema, schema.TableEventQueue)
	if err != nil {
		return "", err
	}
	events, err := qualifiedServiceTable(serviceSchema, schema.TableEvents)
	if err != nil {
		return "", err
	}
	return "UPDATE " + queue + " q\n" +
		"  SET status=" + quoteLiteral("delivering") + ", leased_by=$1, leased_until=now()+$2,\n" +
		"      attempts = attempts + 1\n" +
		"WHERE q.event_id IN (\n" +
		"  SELECT event_id FROM " + queue + "\n" +
		"   WHERE status=" + quoteLiteral("pending") + " AND next_attempt_at <= now()\n" +
		"   ORDER BY next_attempt_at, event_id\n" +
		"   LIMIT $3 FOR UPDATE SKIP LOCKED)\n" +
		"RETURNING q.event_id;        -- payloads then fetched from " + events + " by id\n", nil
}

var _ EventSource = (*TriggerSource)(nil)

// Claim marks eligible queue rows delivering in the committed one-transaction shape, then reads
// their immutable event payloads before committing the lease and the returned batch together.
func (s *TriggerSource) Claim(ctx context.Context, n int) ([]Event, error) {
	if err := s.ensureUsable(); err != nil {
		return nil, err
	}
	if n <= 0 {
		return []Event{}, nil
	}
	// Derived before the transaction opens: an unusable service schema is the caller's mistake and
	// costs nothing to answer, and there is no work to roll back.
	query, err := claimQueryFor(s.cfg.Database.Schema)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rows, err := tx.Query(ctx, query, s.opts.LeasedBy, s.opts.Lease, n)
	if err != nil {
		return nil, fmt.Errorf("claim queue: %w", err)
	}
	ids := make([]int64, 0, n)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read claimed id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read claimed ids: %w", err)
	}
	rows.Close()
	if len(ids) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty claim: %w", err)
		}
		return []Event{}, nil
	}
	events, err := readClaimedEvents(ctx, tx, s.cfg.Database.Schema, ids)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}
	return events, nil
}

func readClaimedEvents(ctx context.Context, tx pgx.Tx, serviceSchema string, ids []int64) ([]Event, error) {
	queue, err := qualifiedServiceTable(serviceSchema, schema.TableEventQueue)
	if err != nil {
		return nil, err
	}
	eventsTable, err := qualifiedServiceTable(serviceSchema, schema.TableEvents)
	if err != nil {
		return nil, err
	}
	query := "SELECT e.id, e.occurred_at, e.listener, e.operation, e.table_name, e.txid, e.payload, q.attempts " +
		"FROM " + queue + " q JOIN " + eventsTable + " e ON e.id=q.event_id AND e.occurred_at=q.occurred_at " +
		"WHERE q.event_id = ANY($1)"
	rows, err := tx.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("read claimed events: %w", err)
	}
	defer rows.Close()
	byID := make(map[int64]Event, len(ids))
	for rows.Next() {
		var event Event
		if err := rows.Scan(
			&event.ID, &event.OccurredAt, &event.Listener, &event.Operation, &event.Table,
			&event.TXID, &event.Payload, &event.Attempt,
		); err != nil {
			return nil, fmt.Errorf("scan claimed event: %w", err)
		}
		byID[event.ID] = event
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read event rows: %w", err)
	}
	ordered := make([]Event, 0, len(byID))
	for _, id := range ids {
		if event, ok := byID[id]; ok {
			ordered = append(ordered, event)
		}
	}
	return ordered, nil
}

func (s *TriggerSource) ensureUsable() error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrSourceClosed
	}
	if s.pool == nil {
		return errors.New("event source has no pool")
	}
	return nil
}

func qualifiedServiceTable(serviceSchema, table string) (string, error) {
	qualified, fault := schema.Qualified(serviceSchema, table)
	if fault != schema.IdentifierOK {
		return "", unusableIdentifierError{field: "service table", value: table, reason: fault}
	}
	return qualified, nil
}
