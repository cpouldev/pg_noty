//go:build integration

package delivery

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
)

type timedTrigger struct {
	*source.TriggerSource
	mu    sync.Mutex
	calls []time.Time
}

func (s *timedTrigger) Claim(ctx context.Context, n int) ([]source.Event, error) {
	s.mu.Lock()
	s.calls = append(s.calls, time.Now())
	s.mu.Unlock()
	return s.TriggerSource.Claim(ctx, n)
}
func (s *timedTrigger) reset()     { s.mu.Lock(); s.calls = nil; s.mu.Unlock() }
func (s *timedTrigger) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.calls) }
func (s *timedTrigger) times() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.calls...)
}

func TestReplicaNotificationClaimsAreJittered(t *testing.T) {
	db := newDeliveryDatabase(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var replicas []*timedTrigger
	var done []chan struct{}
	for i := 0; i < 3; i++ {
		raw, err := source.Open(
			ctx,
			db.pool,
			db.cfg,
			source.Options{LeasedBy: "jitter-" + string(rune('a'+i)), Lease: time.Minute, Listen: true},
		)
		if err != nil {
			t.Fatal(err)
		}
		replica := &timedTrigger{TriggerSource: raw}
		replicas = append(replicas, replica)
		d := time.Duration(i) * 80 * time.Millisecond
		worker := NewWorker(
			replica,
			replica,
			WorkerConfig{PollInterval: time.Second, Jitter: func(int64) int64 { return int64(d) }},
		)
		finished := make(chan struct{})
		done = append(done, finished)
		go func() { _ = worker.Run(ctx); close(finished) }()
	}
	for _, replica := range replicas {
		for deadline := time.Now().Add(time.Second); replica.count() == 0 && time.Now().Before(deadline); {
			time.Sleep(time.Millisecond)
		}
		replica.reset()
	}
	time.Sleep(250 * time.Millisecond)
	channel := "pg_noty_events_" + db.cfg.Instance
	if _, err := db.pool.Exec(t.Context(), "SELECT pg_notify($1,'jitter')", channel); err != nil {
		t.Fatal(err)
	}
	for _, replica := range replicas {
		for deadline := time.Now().Add(3 * time.Second); replica.count() == 0 && time.Now().Before(deadline); {
			time.Sleep(5 * time.Millisecond)
		}
	}
	var first, last time.Time
	for _, replica := range replicas {
		calls := replica.times()
		if len(calls) == 0 {
			t.Fatal("replica missed the notification")
		}
		if first.IsZero() || calls[0].Before(first) {
			first = calls[0]
		}
		if calls[0].After(last) {
			last = calls[0]
		}
	}
	if last.Sub(first) < 50*time.Millisecond {
		t.Fatalf("claim spread=%s, want jittered replicas", last.Sub(first))
	}
	cancel()
	for i, raw := range replicas {
		_ = raw.Close()
		<-done[i]
	}
}

type gateClient struct {
	hold <-chan struct{}
	done chan<- struct{}
	once sync.Once
}

func (c *gateClient) Do(*http.Request) (*http.Response, error) {
	if c.hold != nil {
		<-c.hold
	}
	if c.done != nil {
		c.once.Do(func() { close(c.done) })
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
}

func TestFastListenerCompletesWhileCappedSlowListenerIsHeld(t *testing.T) {
	db := newDeliveryDatabase(t)
	seedDeliveryEvent(t, db, "slow", `{"id":40}`)
	seedDeliveryEvent(t, db, "fast", `{"id":41}`)
	src := openDeliverySource(t, db, "fairness", time.Minute)
	claimed, err := src.Claim(t.Context(), 2)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim=%#v err=%v", claimed, err)
	}
	hold, fastDone := make(chan struct{}), make(chan struct{})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			GlobalConcurrency: 2, Listeners: []ListenerConfig{
				{
					Name: "slow", URL: "http://slow.invalid", Concurrency: 1, Client: &gateClient{hold: hold},
					Policy: Policy{MaxAttempts: 1},
				}, {
					Name: "fast", URL: "http://fast.invalid", Client: &gateClient{done: fastDone},
					Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	for _, event := range claimed {
		if !worker.Dispatch(t.Context(), event) {
			t.Fatal("dispatch refused")
		}
	}
	select {
	case <-fastDone:
	case <-time.After(time.Second):
		t.Fatal("fast listener starved behind capped listener")
	}
	close(hold)
	worker.Wait()
}

func TestCleanDrainStopsTheRunningClaimLoop(t *testing.T) {
	db := newDeliveryDatabase(t)
	seedDeliveryEvent(t, db, "orders", `{"id":50}`)
	seedDeliveryEvent(t, db, "orders", `{"id":51}`)
	seedDeliveryEvent(t, db, "orders", `{"id":52}`)
	src := openDeliverySource(t, db, "drain-loop", time.Minute)
	hold := make(chan struct{})
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK, Hold: hold})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			BatchSize: 1, PollInterval: 10 * time.Millisecond, Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: http.DefaultClient, Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { _ = worker.Run(ctx); close(done) }()
	for deadline := time.Now().Add(time.Second); len(fixture.Requests()) == 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if len(fixture.Requests()) == 0 {
		t.Fatal("run loop did not claim an event")
	}
	var before int
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT count(*) FROM "+deliveryTable(t, db, schema.TableEventQueue)+" WHERE attempts>0",
	).Scan(&before); err != nil {
		t.Fatal(err)
	}
	verdict := worker.Drain(t.Context(), 20*time.Millisecond)
	if !verdict.TimedOut {
		t.Fatal("held run did not report a timed drain")
	}
	close(hold)
	cancel()
	<-done
	worker.Wait()
	var after int
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT count(*) FROM "+deliveryTable(t, db, schema.TableEventQueue)+" WHERE attempts>0",
	).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before || before != 1 {
		t.Fatalf("claims before=%d after=%d, want one claim and no claims after drain", before, after)
	}
}
