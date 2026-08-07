package delivery

import (
	"context"
	"time"
)

const defaultLeaseTimeout = 5 * time.Minute

// leaseHoldLimit leaves a small safety margin before the source lease expires.
// A held event is abandoned locally at this bound; it is not released and is
// never dispatched after the source reclaim sweep can see it as expired.
func (w *Worker) leaseHoldLimit() time.Duration {
	lease := w.config.LeaseTimeout
	if lease <= 0 {
		lease = defaultLeaseTimeout
	}
	margin := lease / 10
	if margin < time.Millisecond {
		margin = time.Millisecond
	}
	if margin >= lease {
		margin = lease / 2
	}
	return lease - margin
}

func (w *Worker) pollInterval() time.Duration {
	interval := w.config.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	bound := w.leaseHoldLimit() / 2
	if bound <= 0 {
		bound = w.leaseHoldLimit()
	}
	if interval > bound {
		return bound
	}
	return interval
}

func (w *Worker) takePending() []Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	var events []Event
	for name, queued := range w.pending {
		events = append(events, queued...)
		delete(w.pending, name)
	}
	return events
}

func (w *Worker) retainPending(queues map[string][]Event) {
	if w.draining() {
		var events []Event
		for _, queued := range queues {
			events = append(events, queued...)
		}
		_, err := w.releaseHeld(context.Background(), events, nil)
		w.noteDrainError(err)
		return
	}
	now := w.now()
	w.mu.Lock()
	defer w.mu.Unlock()
	for name, queued := range queues {
		for _, event := range queued {
			if _, held := w.held[event.ID]; !held {
				w.held[event.ID] = now
			}
			w.pending[name] = append(w.pending[name], event)
		}
	}
}

func (w *Worker) retainEvents(events []Event) {
	queues := make(map[string][]Event)
	for _, event := range events {
		queues[event.Listener] = append(queues[event.Listener], event)
	}
	w.retainPending(queues)
}

func (w *Worker) dropExpiredHeld(events []Event) ([]Event, bool) {
	now := w.now()
	limit := w.leaseHoldLimit()
	kept := events[:0]
	expired := false
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, event := range events {
		since, held := w.held[event.ID]
		if held && now.Sub(since) >= limit {
			delete(w.held, event.ID)
			expired = true
			continue
		}
		kept = append(kept, event)
	}
	return kept, expired
}

func (w *Worker) releaseHold(event Event) {
	w.mu.Lock()
	delete(w.held, event.ID)
	w.mu.Unlock()
}

func (w *Worker) isHeld(id int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, held := w.held[id]
	return held
}
