//go:build integration

package delivery

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
)

func openDeliverySource(t *testing.T, db deliveryDB, owner string, lease time.Duration) *source.TriggerSource {
	t.Helper()
	opened, err := source.Open(
		t.Context(),
		db.pool,
		db.cfg,
		source.Options{LeasedBy: owner, Lease: lease, Listen: false},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

func deliveryListener(name, endpoint string, policy Policy) ListenerConfig {
	return ListenerConfig{
		Name: name, URL: endpoint, Method: "POST", Timeout: time.Second,
		Policy: policy, Client: http.DefaultClient,
	}
}
func TestNoTransactionOverHTTP(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":3}`)
	src := openDeliverySource(t, db, "worker", time.Second)
	claimed, err := src.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != event.ID {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	hold := make(chan struct{})
	fixture := newHTTPFixture(t, HTTPReply{Status: 200, Hold: hold})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{Listeners: []ListenerConfig{deliveryListener("orders", fixture.URL(), Policy{MaxAttempts: 1})}},
	)
	if !worker.Dispatch(t.Context(), claimed[0]) {
		t.Fatal("delivery was not admitted")
	}
	for attempt := 0; attempt < 40 && len(fixture.Requests()) < 1; attempt++ {
		time.Sleep(5 * time.Millisecond)
	}
	var openTransactions int
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'",
	).Scan(&openTransactions); err != nil {
		t.Fatal(err)
	}
	if openTransactions != 0 {
		t.Fatalf("idle transactions during HTTP=%d, want zero", openTransactions)
	}
	close(hold)
	worker.Wait()
}

func TestReclaimSweepRunsBeforeClaimAndSettlesTheStrandedEvent(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":1}`)
	first := openDeliverySource(t, db, "crashed", time.Second)
	claimed, err := first.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("initial claim = %#v, %v", claimed, err)
	}
	queue := deliveryTable(t, db, schema.TableEventQueue)
	if _, err := db.pool.Exec(
		t.Context(),
		"UPDATE "+queue+" SET leased_until=now()-interval '1 second' WHERE event_id=$1",
		event.ID,
	); err != nil {
		t.Fatal(err)
	}
	if recovered, err := first.Claim(t.Context(), 1); err != nil || len(recovered) != 0 {
		t.Fatalf("claim without sweep = %#v, %v; negative control must remain stranded", recovered, err)
	}
	if swept, err := first.ReclaimExpired(t.Context()); err != nil || swept != 1 {
		t.Fatalf("reclaim = %d, %v", swept, err)
	}
	if present, status, attempts, _, _ := queueState(
		t,
		db,
		event.ID,
	); !present || status != "pending" || attempts != 1 {
		t.Fatalf("reclaim state present=%t status=%q attempts=%d, want pending/1", present, status, attempts)
	}
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK})
	worker := NewWorker(
		first,
		first,
		WorkerConfig{
			BatchSize: 1, Listeners: []ListenerConfig{deliveryListener("orders", fixture.URL(), Policy{MaxAttempts: 2})},
		},
	)
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	if len(fixture.Requests()) != 1 {
		t.Fatalf("reclaimed requests=%d, want one", len(fixture.Requests()))
	}
	if present, status, attempts, _, _ := queueState(t, db, event.ID); present || status != "" || attempts != 0 {
		t.Fatalf("reconciled queue present=%t status=%q attempts=%d, want deleted", present, status, attempts)
	}
}

func TestCleanDrainWaitsForInFlightAndStopsClaims(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":10}`)
	src := openDeliverySource(t, db, "worker", time.Minute)
	claimed, err := src.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	hold := make(chan struct{})
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK, Hold: hold})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{Listeners: []ListenerConfig{deliveryListener("orders", fixture.URL(), Policy{MaxAttempts: 1})}},
	)
	if !worker.Dispatch(t.Context(), claimed[0]) {
		t.Fatal("delivery was not admitted")
	}
	result := make(chan DrainResult, 1)
	go func() { result <- worker.Drain(t.Context(), time.Second) }()
	for attempt := 0; attempt < 40 && len(fixture.Requests()) < 1; attempt++ {
		time.Sleep(5 * time.Millisecond)
	}
	close(hold)
	verdict := <-result
	worker.Wait()
	if !verdict.Success() || verdict.TimedOut {
		t.Fatalf("clean drain verdict=%+v", verdict)
	}
	if present, status, _, _, _ := queueState(t, db, event.ID); present || status != "" {
		t.Fatalf("clean drain queue present=%t status=%q, want delivered", present, status)
	}
}

func TestTimedOutDrainReleasesHeldLeaseAndReportsTimeout(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":11}`)
	src := openDeliverySource(t, db, "worker", time.Minute)
	claimed, err := src.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	hold := make(chan struct{})
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK, Hold: hold})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{Listeners: []ListenerConfig{deliveryListener("orders", fixture.URL(), Policy{MaxAttempts: 1})}},
	)
	if !worker.Dispatch(t.Context(), claimed[0]) {
		t.Fatal("delivery was not admitted")
	}
	for attempt := 0; attempt < 40 && len(fixture.Requests()) < 1; attempt++ {
		time.Sleep(5 * time.Millisecond)
	}
	verdict := worker.Drain(t.Context(), 20*time.Millisecond)
	if !verdict.TimedOut || !errors.Is(verdict.Err, ErrDrainTimedOut) {
		t.Fatalf("timeout drain verdict=%+v, want explicit timeout", verdict)
	}
	present, status, _, leasedUntil, leasedBy := queueState(t, db, event.ID)
	if !present || status != "pending" || leasedUntil != nil || leasedBy != nil {
		t.Fatalf("released lease present=%t status=%q until=%v by=%v", present, status, leasedUntil, leasedBy)
	}
	close(hold)
	worker.Wait()
}

type cancelAfterResponse struct{ cancel context.CancelFunc }

func (c cancelAfterResponse) Do(*http.Request) (*http.Response, error) {
	c.cancel()
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
}

func TestCanceledDeliveryContextStillRecordsTheOutcome(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":12}`)
	src := openDeliverySource(t, db, "worker", time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			Listeners: []ListenerConfig{
				{
					Name: "orders", Client: cancelAfterResponse{cancel}, Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	if present, status, _, _, _ := queueState(t, db, event.ID); present || status != "" {
		t.Fatalf("canceled context queue present=%t status=%q, want delivered", present, status)
	}
}
