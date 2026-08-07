package delivery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/source"
)

func TestDrainCleanWaitsForInFlightAndRecordsOutcome(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	src := &drainSource{event: testEvent(90, "orders", []byte(`{}`))}
	client := &workerHTTP{factory: okResponse, started: started, block: release}
	worker := NewWorker(src, nil, WorkerConfig{Listeners: []ListenerConfig{testListener("orders", 1, 1024, client)}})
	if !worker.Dispatch(context.Background(), src.event) {
		t.Fatal("delivery was not admitted")
	}
	<-started
	result := make(chan DrainResult, 1)
	go func() { result <- worker.Drain(context.Background(), time.Second) }()
	close(release)
	got := <-result
	if !got.Success() || got.TimedOut || len(got.Released) != 0 || src.ackCalls != 1 || src.nackCalls != 0 {
		t.Fatalf("result=%+v ack=%d nack=%d, want clean ack", got, src.ackCalls, src.nackCalls)
	}
}

func TestDrainTimeoutReleasesInFlightLeaseAndReportsFailure(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	src := &drainSource{event: testEvent(91, "orders", []byte(`{}`))}
	client := &workerHTTP{factory: okResponse, started: started, block: release}
	worker := NewWorker(src, nil, WorkerConfig{Listeners: []ListenerConfig{testListener("orders", 1, 1024, client)}})
	if !worker.Dispatch(context.Background(), src.event) {
		t.Fatal("delivery was not admitted")
	}
	<-started
	got := worker.Drain(context.Background(), 10*time.Millisecond)
	if !got.TimedOut || got.Success() || !errors.Is(
		got.Err,
		ErrDrainTimedOut,
	) || len(got.Released) != 1 || got.Released[0] != src.event.ID || src.nackCalls != 1 {
		t.Fatalf("result=%+v nack=%d, want timed-out release", got, src.nackCalls)
	}
	close(release)
	worker.Wait()
}

func TestDrainStopsClaimsAndReleasesHeldEvents(t *testing.T) {
	src := &drainSource{event: testEvent(92, "slow", []byte(`{}`))}
	client := &workerHTTP{response: okResponse()}
	worker := NewWorker(
		src,
		nil,
		WorkerConfig{BatchSize: 1, Listeners: []ListenerConfig{testListener("slow", 1, 1024, client)}},
	)
	if !worker.listeners["slow"].sem.tryAcquire() {
		t.Fatal("failed to saturate listener")
	}
	runWorker(t, worker)
	if len(worker.pending["slow"]) != 1 {
		t.Fatal("event was not held before drain")
	}
	if got := worker.Drain(
		context.Background(),
		time.Second,
	); !got.Success() || len(got.Released) != 1 || got.Released[0] != src.event.ID || src.nackCalls != 1 {
		t.Fatalf("result=%+v nack=%d, want clean pending release", got, src.nackCalls)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if src.claimCalls != 1 {
		t.Fatalf("claims=%d after drain, want one initial claim", src.claimCalls)
	}
}

type drainSource struct {
	mu         sync.Mutex
	event      Event
	claimCalls int
	ackCalls   int
	nackCalls  int
	released   bool
}

func (s *drainSource) Claim(context.Context, int) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimCalls++
	if s.claimCalls > 1 || s.released {
		return nil, nil
	}
	return []Event{s.event}, nil
}
func (s *drainSource) Ack(context.Context, Event, Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackCalls++
	if s.released {
		return source.ErrEventNotClaimed
	}
	return nil
}
func (s *drainSource) Nack(context.Context, Event, Delivery, time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nackCalls++
	s.released = true
	return nil
}
func (s *drainSource) Dead(context.Context, Event, Delivery, string) error { return nil }
func (s *drainSource) Notify() <-chan struct{}                             { return nil }
func (s *drainSource) Close() error                                        { return nil }
