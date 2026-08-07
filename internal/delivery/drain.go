package delivery

import (
	"context"
	"errors"
	"sync"
	"time"
)

const defaultDrainTimeout = 30 * time.Second

// ErrDrainTimedOut is returned in DrainResult.Err when the independent drain
// bound elapsed before all requests finished.  internal/cli maps this verdict
// to its process exit status.
var ErrDrainTimedOut = errors.New("delivery drain timed out")

// DrainResult is the honest lifecycle verdict. Released contains event ids
// whose worker-owned lease was successfully transitioned back to pending.
type DrainResult struct {
	Clean    bool
	TimedOut bool
	Released []int64
	Err      error
}

// Success reports a clean drain with no failed lease-release transition.
func (r DrainResult) Success() bool { return r.Clean && !r.TimedOut && r.Err == nil }

type workerActivity struct {
	mu         sync.Mutex
	events     map[int64][]Event
	changed    chan struct{}
	stopping   bool
	releaseErr error
}

// activityFor is the worker's own drain state, created on first use. It was a package-level sync.Map
// keyed by *Worker that nothing deleted from, so every Worker ever built -- and its transports and
// signing secrets -- stayed reachable for the process lifetime.
func activityFor(w *Worker) *workerActivity {
	w.activityOnce.Do(func() {
		w.activity = &workerActivity{events: make(map[int64][]Event), changed: make(chan struct{})}
	})
	return w.activity
}

func (w *Worker) draining() bool {
	if w == nil {
		return true
	}
	state := activityFor(w)
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.stopping
}

func (w *Worker) track(event Event) bool {
	state := activityFor(w)
	state.mu.Lock()
	if state.stopping {
		state.mu.Unlock()
		return false
	}
	state.events[event.ID] = append(state.events[event.ID], event)
	w.wg.Add(1)
	state.mu.Unlock()
	return true
}

func (w *Worker) untrack(event Event) {
	state := activityFor(w)
	state.mu.Lock()
	queued := state.events[event.ID]
	if len(queued) <= 1 {
		delete(state.events, event.ID)
	} else {
		state.events[event.ID] = queued[1:]
	}
	old := state.changed
	state.changed = make(chan struct{})
	close(old)
	state.mu.Unlock()
	w.finish(event.ID)
	w.wg.Done()
}

// Drain stops new claims, waits for in-flight work under an independent bound,
// and releases all events still owned by this worker.  The supplied context is
// used only as the caller's source for a detached transition; cancellation does
// not shorten the drain bound.
func (w *Worker) Drain(ctx context.Context, timeout time.Duration) DrainResult {
	if w == nil {
		return DrainResult{Clean: true}
	}
	state := activityFor(w)
	state.mu.Lock()
	state.stopping = true
	state.mu.Unlock()
	if timeout <= 0 {
		timeout = w.config.DrainTimeout
	}
	if timeout <= 0 {
		timeout = defaultDrainTimeout
	}
	timedOut := waitForActivity(state, timeout)
	events := append(activityEvents(state), w.takeHeldForDrain()...)
	result := DrainResult{Clean: !timedOut, TimedOut: timedOut}
	if timedOut {
		result.Err = ErrDrainTimedOut
	}
	result.Released, result.Err = w.releaseHeld(ctx, events, result.Err)
	state.mu.Lock()
	result.Err = errors.Join(result.Err, state.releaseErr)
	state.mu.Unlock()
	// Recorded where a later drain reads it: takeHeldForDrain is destructive, so the CLI's drain --
	// which runs after the loop's own -- had nothing left to fail on and returned Clean, reporting
	// status 0 for a shutdown whose lease releases had all failed.
	w.noteDrainError(result.Err)
	if result.Err != nil {
		result.Clean = false
	}
	return result
}

func (w *Worker) noteDrainError(err error) {
	if err == nil {
		return
	}
	state := activityFor(w)
	state.mu.Lock()
	state.releaseErr = errors.Join(state.releaseErr, err)
	state.mu.Unlock()
}

func waitForActivity(state *workerActivity, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		state.mu.Lock()
		if len(state.events) == 0 {
			state.mu.Unlock()
			return false
		}
		changed := state.changed
		state.mu.Unlock()
		select {
		case <-changed:
		case <-timer.C:
			return true
		}
	}
}

func activityEvents(state *workerActivity) []Event {
	state.mu.Lock()
	defer state.mu.Unlock()
	var events []Event
	for _, queued := range state.events {
		events = append(events, queued...)
	}
	return events
}

func (w *Worker) takeHeldForDrain() []Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	var events []Event
	for name, queued := range w.pending {
		events = append(events, queued...)
		delete(w.pending, name)
	}
	w.held = make(map[int64]time.Time)
	return events
}

func (w *Worker) releaseHeld(ctx context.Context, events []Event, prior error) ([]int64, error) {
	seen := make(map[int64]struct{}, len(events))
	released := make([]int64, 0, len(events))
	err := prior
	for _, event := range events {
		if _, exists := seen[event.ID]; exists {
			continue
		}
		seen[event.ID] = struct{}{}
		delivery := Delivery{Err: "delivery worker drain", Duration: 0}
		releaseErr := w.events.Nack(detachedContext(w, ctx), event, delivery, w.now())
		if lateClaim(releaseErr) {
			continue
		}
		if releaseErr != nil {
			err = errors.Join(err, releaseErr)
			continue
		}
		released = append(released, event.ID)
	}
	return released, err
}
