package delivery

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestAnInFlightRequestSurvivesCancellationUntilItsOwnTimeout pins the shutdown contract doc.go
// publishes: an attempt already sent is bounded by the listener's timeout, and the wait for it is
// bounded by DrainTimeout. The dispatch goroutine used to carry the command context, which
// signal.NotifyContext cancels the instant SIGTERM arrives, so every in-flight request was aborted
// at once -- waitForActivity returned immediately and DrainTimeout bounded nothing.
func TestAnInFlightRequestSurvivesCancellationUntilItsOwnTimeout(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	client := &contextRespectingHTTP{started: started, release: release}
	src := &workerSource{batch: []Event{testEvent(1, "slow", []byte(`{}`))}}
	worker := NewWorker(src, nil, WorkerConfig{BatchSize: 1, GlobalConcurrency: 1,
		Listeners: []ListenerConfig{testListener("slow", 0, 1024, client)}})

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never started")
	}

	// SIGTERM's effect on the dispatch context, while the request is still open.
	cancel()

	// The request must still be in flight: nothing has released it, and its own timeout has not
	// elapsed. Before the repair the transport aborted here and InFlight fell to zero.
	time.Sleep(50 * time.Millisecond)
	if got := worker.InFlight(); got != 1 {
		t.Fatalf("InFlight = %d after cancellation, want 1; the attempt was aborted rather than "+
			"drained, so DrainTimeout bounds nothing", got)
	}

	close(release)
	worker.Wait()
	if client.calls != 1 {
		t.Errorf("destination saw %d requests, want 1", client.calls)
	}
	if client.cancelled {
		t.Error("the transport observed a cancelled request context; an attempt already sent must " +
			"be bounded by its own timeout, not by the shutdown signal")
	}
	if src.ackCalls != 1 {
		t.Errorf("Ack was called %d times, want 1; the outcome of an attempt that completed during "+
			"the drain must still be recorded", src.ackCalls)
	}
}

// contextRespectingHTTP is what a real transport does and the shared workerHTTP fake does not:
// it abandons the request when its context is cancelled. Without that, this test would pass
// against the very code it exists to fail.
type contextRespectingHTTP struct {
	started   chan<- struct{}
	release   <-chan struct{}
	calls     int
	cancelled bool
}

func (c *contextRespectingHTTP) Do(request *http.Request) (*http.Response, error) {
	c.calls++
	c.started <- struct{}{}
	select {
	case <-c.release:
		return okResponse(), nil
	case <-request.Context().Done():
		c.cancelled = true
		return nil, request.Context().Err()
	}
}
