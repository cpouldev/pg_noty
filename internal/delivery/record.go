package delivery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// RecordOutcome applies exactly one source transition to an attempt.  The
// source records that transition and the nullable status/error fields in its
// short transaction; the worker supplies only source-independent vocabulary.
func (w *Worker) RecordOutcome(ctx context.Context, event Event, policy Policy, response *http.Response, delivery Delivery) error {
	return w.recordResult(ctx, event, policy, response, delivery)
}

func (w *Worker) recordResult(ctx context.Context, event Event, policy Policy, response *http.Response, delivery Delivery) error {
	delivery.Snippet = TruncateSnippet([]byte(delivery.Snippet))
	outcome := classifyAttempt(event, policy, response, delivery)
	detached := detachedContext(w, ctx)
	switch {
	case outcome == OutcomeSuccess:
		return w.settleAck(detached, event, delivery)
	case outcome == OutcomeRetryable:
		clockNow := w.now()
		retryAfter := time.Duration(0)
		if response != nil {
			retryAfter, _ = ParseRetryAfter(response.Header.Get("Retry-After"), clockNow)
		}
		return w.settleNack(detached, event, delivery,
			clockNow.Add(policy.DelayAfter(event.Attempt, retryAfter, w.jitter)))
	default:
		reason := delivery.Err
		if reason == "" {
			reason = fmt.Sprintf("HTTP status %d", delivery.HTTPStatus)
			if delivery.HTTPStatus >= 300 && delivery.HTTPStatus <= 399 {
				reason = fmt.Sprintf("HTTP redirect status %d", delivery.HTTPStatus)
			}
		}
		return w.settleDead(detached, event, delivery, reason)
	}
}

func classifyAttempt(event Event, policy Policy, response *http.Response, delivery Delivery) Outcome {
	outcome := ClassifyError(deliveryError(delivery))
	if delivery.Err == "" && response != nil {
		outcome = ClassifyStatus(response.StatusCode)
	}
	if outcome == OutcomeRetryable && !policy.ShouldRetry(event.Attempt, outcome) {
		return OutcomeTerminal
	}
	return outcome
}

func detachedContext(w *Worker, ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if w != nil && w.detach != nil {
		return w.detach(ctx)
	}
	return context.WithoutCancel(ctx)
}

func (w *Worker) settleAck(ctx context.Context, event Event, delivery Delivery) error {
	if w == nil || w.events == nil {
		return errors.New("delivery worker has no event source")
	}
	return swallowLateClaim(w.events.Ack(ctx, event, delivery))
}

func (w *Worker) settleNack(ctx context.Context, event Event, delivery Delivery, retryAt time.Time) error {
	if w == nil || w.events == nil {
		return errors.New("delivery worker has no event source")
	}
	return swallowLateClaim(w.events.Nack(ctx, event, delivery, retryAt))
}

func (w *Worker) settleDead(ctx context.Context, event Event, delivery Delivery, reason string) error {
	if w == nil || w.events == nil {
		return errors.New("delivery worker has no event source")
	}
	return swallowLateClaim(w.events.Dead(ctx, event, delivery, reason))
}
