package delivery

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRecordOutcomeMapsEachResultOnceWithDetachedContext(t *testing.T) {
	cases := []struct {
		name        string
		response    int
		deliveryErr string
		want        string
	}{
		{name: "success", response: 204, want: "ack"},
		{name: "retry", response: 503, want: "nack"},
		{name: "terminal", response: 400, want: "dead"},
		{name: "transport", deliveryErr: "connection reset", want: "nack"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := &recordSource{}
			clock := time.Unix(500, 0)
			worker := NewWorker(src, nil, WorkerConfig{Now: func() time.Time { return clock }})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			response := responseWithStatus(tc.response)
			delivery := Delivery{HTTPStatus: tc.response, Snippet: strings.Repeat("x", maxSnippetBytes+64), Err: tc.deliveryErr}
			if err := worker.RecordOutcome(ctx, testEvent(80, "orders", []byte(`{}`)), Policy{MaxAttempts: 3, InitialInterval: time.Second, MaxInterval: time.Second}, response, delivery); err != nil {
				t.Fatal(err)
			}
			if got := src.transition(); got != tc.want {
				t.Fatalf("transition=%q, want %q", got, tc.want)
			}
			if src.ackCalls+src.nackCalls+src.deadCalls != 1 {
				t.Fatalf("transition calls ack=%d nack=%d dead=%d, want exactly one", src.ackCalls, src.nackCalls, src.deadCalls)
			}
			if tc.want == "nack" && !src.retryAt.Equal(clock.Add(time.Second)) {
				t.Fatalf("retryAt=%s, want %s", src.retryAt, clock.Add(time.Second))
			}
			if src.contextErr != nil {
				t.Fatalf("source context=%v, want detached", src.contextErr)
			}
			if len(src.lastDelivery.Snippet) > maxSnippetBytes {
				t.Fatalf("snippet bytes=%d, want <=%d", len(src.lastDelivery.Snippet), maxSnippetBytes)
			}
		})
	}
}

func TestRecordOutcomeKeepsNullableStatusForTransportError(t *testing.T) {
	src := &recordSource{}
	worker := NewWorker(src, nil, WorkerConfig{Now: time.Now})
	if err := worker.RecordOutcome(context.Background(), testEvent(81, "orders", []byte(`{}`)), Policy{MaxAttempts: 2, InitialInterval: time.Second, MaxInterval: time.Second}, nil, Delivery{Err: "timeout"}); err != nil {
		t.Fatal(err)
	}
	if src.lastDelivery.HTTPStatus != 0 || src.lastDelivery.Err != "timeout" {
		t.Fatalf("delivery=%+v, want null status and preserved error", src.lastDelivery)
	}
}

type recordSource struct {
	ackCalls, nackCalls, deadCalls int
	lastDelivery                   Delivery
	contextErr                     error
	retryAt                        time.Time
	last                           string
}

func (s *recordSource) transition() string { return s.last }
func (s *recordSource) capture(ctx context.Context, d Delivery, name string) {
	s.contextErr, s.lastDelivery, s.last = ctx.Err(), d, name
}
func (s *recordSource) Claim(context.Context, int) ([]Event, error) { return nil, nil }
func (s *recordSource) Ack(ctx context.Context, _ Event, d Delivery) error {
	s.ackCalls++
	s.capture(ctx, d, "ack")
	return nil
}
func (s *recordSource) Nack(ctx context.Context, _ Event, d Delivery, retryAt time.Time) error {
	s.nackCalls++
	s.retryAt = retryAt
	s.capture(ctx, d, "nack")
	return nil
}
func (s *recordSource) Dead(ctx context.Context, _ Event, d Delivery, _ string) error {
	s.deadCalls++
	s.capture(ctx, d, "dead")
	return nil
}
func (s *recordSource) Notify() <-chan struct{}                       { return nil }
func (s *recordSource) Close() error                                  { return errors.New("unused") }
func (s *recordSource) ReclaimExpired(context.Context) (int64, error) { return 0, nil }

func responseWithStatus(status int) *http.Response {
	if status == 0 {
		return nil
	}
	return &http.Response{StatusCode: status, Header: make(http.Header)}
}
