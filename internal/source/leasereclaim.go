package source

import (
	"context"
	"fmt"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// LeaseReclaimer is the sibling capability that returns abandoned deliveries to the
// pending queue. It is deliberately separate from EventSource: reclaim is a liveness
// sweep, not another claim or outcome transition.
type LeaseReclaimer interface {
	ReclaimExpired(context.Context) (int64, error)
}

var _ LeaseReclaimer = (*TriggerSource)(nil)

// ReclaimExpired returns every delivering queue row whose lease has expired to pending.
// PostgreSQL's now() is the authority for the expiry comparison, and the partial
// IndexQueueLeaseReclaim index covers the status predicate. Attempts are intentionally
// untouched; a subsequent Claim increments them exactly once for the redelivery.
func (s *TriggerSource) ReclaimExpired(ctx context.Context) (int64, error) {
	if err := s.ensureUsable(); err != nil {
		return 0, err
	}
	queue, err := qualifiedServiceTable(s.cfg.Database.Schema, schema.TableEventQueue)
	if err != nil {
		return 0, err
	}
	// Keep this predicate aligned with schema.IndexQueueLeaseReclaim, the partial
	// index provisioned for delivering rows. The server clock must decide expiry.
	query := "UPDATE " + queue + " SET status=" + quoteLiteral("pending") +
		", leased_by=NULL, leased_until=NULL WHERE status=" + quoteLiteral("delivering") +
		" AND leased_until < now()"
	tag, err := s.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("reclaim expired leases: %w", err)
	}
	return tag.RowsAffected(), nil
}
