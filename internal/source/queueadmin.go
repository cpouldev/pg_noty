package source

import (
	"context"
	"fmt"
	"time"
)

// QueueSelector is the operator's queue selection vocabulary. ID is the single-event form;
// Listener and Status are the list and batch form.
type QueueSelector struct {
	ID       *int64
	Listener string
	Status   string
}

// QueueObservation is one bounded snapshot of queue state.
type QueueObservation struct {
	Pending          int64
	Delivering       int64
	Dead             int64
	OldestPendingAge time.Duration
	DeadByListener   map[string]int64
}

// QueueEvent is the mutable queue state joined to its immutable event row.
type QueueEvent struct {
	ID            int64
	OccurredAt    time.Time
	Listener      string
	Operation     string
	Table         string
	Payload       []byte
	Status        string
	Attempts      int
	NextAttemptAt time.Time
	DeadReason    *string
}

// QueueAdmin is the operator-facing sibling of EventSource. EventSource remains the delivery port.
type QueueAdmin interface {
	ObserveQueue(context.Context) (QueueObservation, error)
	ListEvents(context.Context, QueueSelector) ([]QueueEvent, error)
	RetryBatch(context.Context, QueueSelector, int) (int, error)
}

func validateStatus(status string) error {
	if status == "" || status == "pending" || status == "delivering" || status == "dead" {
		return nil
	}
	return fmt.Errorf("unknown queue status %q", status)
}
