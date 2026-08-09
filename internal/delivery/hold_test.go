package delivery

import (
	"context"
	"testing"
	"time"
)

func TestCappedHoldIsDroppedBeforeItsLeaseCanBeReclaimed(t *testing.T) {
	clock := &holdClock{at: time.Unix(100, 0)}
	src := &holdProbeSource{now: clock.Now, lease: 10 * time.Millisecond}
	client := &workerHTTP{}
	worker := NewWorker(src, src, WorkerConfig{
		BatchSize: 1, GlobalConcurrency: 1, LeaseTimeout: src.lease, Now: clock.Now,
		Listeners: []ListenerConfig{testListener("slow", 1, 1024, client)},
	})
	src.worker = worker
	if !worker.listeners["slow"].sem.tryAcquire() {
		t.Fatal("failed to saturate slow listener")
	}
	src.event = testEvent(31, "slow", []byte(`{}`))
	runWorker(t, worker)
	if !worker.isHeld(src.event.ID) {
		t.Fatal("capped event was not retained")
	}
	clock.at = clock.at.Add(src.lease)
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if src.reclaimWhileHeld {
		t.Fatal("worker reclaim swept an event it was still holding")
	}
	if src.claims != 1 {
		t.Fatalf("expired held event was claimed %d times in the same cycle, want 1", src.claims)
	}
	if client.calls != 0 {
		t.Fatalf("capped event made %d requests after lease bound", client.calls)
	}
}

func TestPollIntervalIsBoundedByTheHeldLease(t *testing.T) {
	worker := NewWorker(nil, nil, WorkerConfig{PollInterval: time.Hour, LeaseTimeout: 20 * time.Millisecond})
	if got := worker.pollInterval(); got >= worker.config.LeaseTimeout {
		t.Fatalf("poll interval = %s, want below lease timeout %s", got, worker.config.LeaseTimeout)
	}
}

type holdClock struct{ at time.Time }

func (c *holdClock) Now() time.Time { return c.at }

type holdProbeSource struct {
	event            Event
	now              Clock
	lease            time.Duration
	worker           *Worker
	status           string
	leasedUntil      time.Time
	claims           int
	reclaimWhileHeld bool
}

func (s *holdProbeSource) ReclaimExpired(context.Context) (int64, error) {
	if s.status == "delivering" && !s.now().Before(s.leasedUntil) {
		if s.worker != nil && s.worker.isHeld(s.event.ID) {
			s.reclaimWhileHeld = true
		}
		s.status = "pending"
		return 1, nil
	}
	return 0, nil
}

func (s *holdProbeSource) Claim(context.Context, int) ([]Event, error) {
	if s.status != "pending" && s.claims > 0 {
		return nil, nil
	}
	s.status = "delivering"
	s.leasedUntil = s.now().Add(s.lease)
	s.claims++
	return []Event{s.event}, nil
}

func (s *holdProbeSource) Ack(context.Context, Event, Delivery) error             { return nil }
func (s *holdProbeSource) Nack(context.Context, Event, Delivery, time.Time) error { return nil }
func (s *holdProbeSource) Dead(context.Context, Event, Delivery, string) error    { return nil }
func (s *holdProbeSource) Notify() <-chan struct{}                                { return nil }
func (s *holdProbeSource) Close() error                                           { return nil }
