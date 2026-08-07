package delivery

import (
	"context"
	"errors"
)

func (w *Worker) dispatchBatch(ctx context.Context, events []Event) int {
	// Claim's frozen port cannot filter listeners; retain blocked rows locally so a cap never
	// burns another claim while round-robin admission continues for uncapped listeners.
	queues := make(map[string][]Event, len(events))
	for _, event := range events {
		queues[event.Listener] = append(queues[event.Listener], event)
	}
	order := w.listenerNames(queues)
	accepted := 0
	for {
		progress := false
		for _, name := range order {
			pending := queues[name]
			if len(pending) == 0 {
				continue
			}
			state := w.listeners[name]
			if state == nil {
				queues[name] = pending[1:]
				w.releaseHold(pending[0])
				w.consumeUnknown(ctx, pending[0])
				progress = true
				continue
			}
			result := w.tryDispatch(ctx, pending[0], state)
			if result == dispatchBlocked {
				continue
			}
			queues[name] = pending[1:]
			w.releaseHold(pending[0])
			progress = true
			if result == dispatchAccepted {
				accepted++
			}
		}
		if !progress {
			w.retainPending(queues)
			return accepted
		}
		if allQueuesEmpty(queues) {
			return accepted
		}
	}
}
func (w *Worker) dispatchEvent(ctx context.Context, event Event, state *listenerState) bool {
	if state == nil {
		w.consumeUnknown(ctx, event)
		return false
	}
	return w.tryDispatch(ctx, event, state) == dispatchAccepted
}
func (w *Worker) tryDispatch(ctx context.Context, event Event, state *listenerState) dispatchState {
	if state == nil || w.draining() {
		return dispatchBlocked
	}
	if state.config.MaxPayloadBytes > 0 {
		if ok, reason := checkPayloadSize(event.Payload, state.config.MaxPayloadBytes); !ok {
			w.observeSettlement(ctx, event, state.config, w.settleDead(w.detach(ctx), event, Delivery{Err: reason}, reason))
			return dispatchConsumed
		}
	}
	if !w.global.tryAcquire() {
		return dispatchBlocked
	}
	if !state.sem.tryAcquire() {
		w.global.release()
		return dispatchBlocked
	}
	w.mu.Lock()
	w.inFlight[event.ID]++
	w.mu.Unlock()
	if !w.track(event) {
		w.finish(event.ID)
		w.global.release()
		state.sem.release()
		return dispatchBlocked
	}
	// The delivery runs on a context detached from cancellation. The command context is cancelled
	// the instant SIGTERM arrives, so passing it here aborted every in-flight request immediately:
	// waitForActivity then returned at once and DrainTimeout bounded nothing, contradicting doc.go's
	// "gives in-flight work an independent drain bound". Each attempt is still bounded, by the
	// listener's own http.Client timeout, and the drain is bounded by DrainTimeout -- which is the
	// pair of bounds the shutdown contract names.
	// TestAnInFlightRequestSurvivesCancellationUntilItsOwnTimeout holds it.
	delivering := w.detach(ctx)
	go func() {
		defer w.global.release()
		defer state.sem.release()
		defer w.untrack(event)
		w.deliver(delivering, event, state)
	}()
	return dispatchAccepted
}
func (w *Worker) Dispatch(ctx context.Context, event Event) bool {
	if w == nil {
		return false
	}
	return w.dispatchEvent(ctx, event, w.listeners[event.Listener])
}
func (w *Worker) finish(id int64) {
	w.mu.Lock()
	if count := w.inFlight[id]; count <= 1 {
		delete(w.inFlight, id)
	} else {
		w.inFlight[id] = count - 1
	}
	w.mu.Unlock()
}
func (w *Worker) deliver(ctx context.Context, event Event, state *listenerState) {
	started := w.now()
	result := DispatchResult{Event: event, Listener: state.config, Started: started}
	body, err := BuildEnvelope(event)
	if err != nil {
		result.Err = err
		result.Outcome = OutcomeTerminal
		result.Finished = w.now()
		settleErr := w.settleDead(w.detach(ctx), event, Delivery{Err: err.Error(), Duration: result.Finished.Sub(started)}, err.Error())
		result.Err = errors.Join(result.Err, settleErr)
		w.observe(ctx, result)
		return
	}
	request, err := buildRequest(ctx, state.config, event, body, w.now())
	if err != nil {
		result.Err = err
		result.Outcome = OutcomeTerminal
		result.Finished = w.now()
		settleErr := w.settleDead(w.detach(ctx), event, Delivery{Err: err.Error(), Duration: result.Finished.Sub(started)}, err.Error())
		result.Err = errors.Join(result.Err, settleErr)
		w.observe(ctx, result)
		return
	}
	result.Request = request
	response, snippet, requestErr := doRequest(state.client, request)
	result.Status, result.Snippet, result.Err = responseStatus(response), snippet, requestErr
	result.Finished = w.now()
	delivery := Delivery{HTTPStatus: result.Status, Snippet: TruncateSnippet(snippet), Duration: result.Finished.Sub(started)}
	if requestErr != nil {
		delivery.Err = requestErr.Error()
	}
	result.Outcome = classifyAttempt(event, state.config.Policy, response, delivery)
	if settleErr := w.recordResult(ctx, event, state.config.Policy, response, delivery); settleErr != nil {
		result.Err = errors.Join(result.Err, settleErr)
	}
	w.observe(ctx, result)
}
func (w *Worker) observe(ctx context.Context, result DispatchResult) {
	if w.hook != nil {
		w.hook(w.detach(ctx), result)
	}
}

func (w *Worker) observeSettlement(ctx context.Context, event Event, listener ListenerConfig, err error) {
	if w.hook == nil {
		return
	}
	started := w.now()
	finished := w.now()
	w.hook(w.detach(ctx), DispatchResult{Event: event, Listener: listener, Err: err,
		Outcome: OutcomeTerminal, Started: started, Finished: finished})
}
