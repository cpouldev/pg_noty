package source

import (
	"context"
	"time"
)

// EventSource is the mechanism-free port consumed by the reconciler and delivery engine.
// internal/reconcile and internal/delivery bind to it.
//
// Claim with no work returns an empty result and a nil error. A late Ack/Nack/Dead returns
// ErrEventNotClaimed; an unreachable database yields an error distinguishable from that sentinel.
// Ack deletes a claimed delivery, Nack schedules its supplied retry instant, and Dead retains a
// claimed row as dead. Those operations do not modify the append-only event log.
//
// Close blocks until the listening goroutine has returned and the dedicated connection is closed,
// but does not wait for in-flight Claim/Ack/Nack/Dead. A lost LISTEN connection costs latency only;
// the source reconnects and callers continue to use the port. Ack/Nack/Dead do not detach the
// caller's context. internal/delivery supplies one that outlives the delivery, and cancellation
// remains recognizable to the caller.
type EventSource interface {
	Claim(ctx context.Context, n int) ([]Event, error)
	Ack(ctx context.Context, e Event, d Delivery) error
	Nack(ctx context.Context, e Event, d Delivery, retryAt time.Time) error
	Dead(ctx context.Context, e Event, d Delivery, reason string) error
	Notify() <-chan struct{}
	Close() error
}
