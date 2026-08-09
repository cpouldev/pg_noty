//go:build integration

package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestRedirectIsRefusedAndItsTargetReceivesNoRequest(t *testing.T) {
	var targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":6}`)
	src := openDeliverySource(t, db, "worker", time.Minute)
	fixture := newHTTPFixture(t, HTTPReply{Redirect: target.URL})
	client := &http.Client{CheckRedirect: refuseRedirects}
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: client, Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target calls=%d, want zero", targetCalls.Load())
	}
	if present, status, _, _, _ := queueState(t, db, event.ID); !present || status != "dead" {
		t.Fatalf("redirect queue present=%t status=%q, want dead", present, status)
	}
}
func TestNackUsesDatabaseClockUnderWorkerSkew(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":13}`)
	src := openDeliverySource(t, db, "worker", time.Minute)
	claimed, err := src.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%#v err=%v", claimed, err)
	}
	skewed := time.Now().Add(2 * time.Hour)
	worker := NewWorker(src, src, WorkerConfig{Now: func() time.Time { return skewed }})
	policy := Policy{MaxAttempts: 3, Backoff: "fixed", InitialInterval: 5 * time.Second, MaxInterval: time.Minute}
	if err := worker.RecordOutcome(
		t.Context(),
		claimed[0],
		policy,
		&http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header)},
		Delivery{HTTPStatus: 503, Err: "retry"},
	); err != nil {
		t.Fatal(err)
	}
	var scheduled, serverNow time.Time
	queue := deliveryTable(t, db, schema.TableEventQueue)
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT next_attempt_at, now() FROM "+queue+" WHERE event_id=$1",
		event.ID,
	).Scan(&scheduled, &serverNow); err != nil {
		t.Fatal(err)
	}
	want := skewed.Add(5 * time.Second).Sub(serverNow)
	if diff := scheduled.Sub(serverNow) - want; diff > time.Second || diff < -time.Second {
		t.Fatalf("stored delay=%s want worker absolute-to-source relative delay %s", scheduled.Sub(serverNow), want)
	}
}
func TestReclaimUsesDatabaseClockDespiteWorkerSkew(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":19}`)
	src := openDeliverySource(t, db, "skew", time.Minute)
	if claimed, err := src.Claim(t.Context(), 1); err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	queue := deliveryTable(t, db, schema.TableEventQueue)
	if _, err := db.pool.Exec(
		t.Context(),
		"UPDATE "+queue+" SET leased_until=now()+interval '1 minute' WHERE event_id=$1",
		event.ID,
	); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(src, src, WorkerConfig{Now: func() time.Time { return time.Now().Add(2 * time.Hour) }})
	if !worker.global.tryAcquire() {
		t.Fatal("failed to reserve claim capacity")
	}
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if present, status, attempts, _, _ := queueState(
		t,
		db,
		event.ID,
	); !present || status != "delivering" || attempts != 1 {
		t.Fatalf("live lease present=%t status=%q attempts=%d, want delivering/1", present, status, attempts)
	}
	if _, err := db.pool.Exec(
		t.Context(),
		"UPDATE "+queue+" SET leased_until=now()-interval '1 second' WHERE event_id=$1",
		event.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if present, status, attempts, _, _ := queueState(
		t,
		db,
		event.ID,
	); !present || status != "pending" || attempts != 1 {
		t.Fatalf("expired lease present=%t status=%q attempts=%d, want pending/1", present, status, attempts)
	}
}
func TestRetryAfterIsAnchoredAsADatabaseRelativeFloor(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":20}`)
	src := openDeliverySource(t, db, "retry-after", time.Minute)
	fixture := newHTTPFixture(
		t,
		HTTPReply{Status: http.StatusServiceUnavailable, Headers: http.Header{"Retry-After": {"1"}}},
	)
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: http.DefaultClient,
					Policy: Policy{MaxAttempts: 2, InitialInterval: time.Millisecond, MaxInterval: 2 * time.Second},
				},
			},
		},
	)
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	var scheduled, serverNow time.Time
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT next_attempt_at, now() FROM "+deliveryTable(t, db, schema.TableEventQueue)+" WHERE event_id=$1",
		event.ID,
	).Scan(&scheduled, &serverNow); err != nil {
		t.Fatal(err)
	}
	if delay := scheduled.Sub(serverNow); delay < 900*time.Millisecond || delay > 2*time.Second {
		t.Fatalf("Retry-After delay=%s, want near one second and capped", delay)
	}
}
func TestUnorderedDeliveryUsesCompletionOrderNotClaimOrder(t *testing.T) {
	db := newDeliveryDatabase(t)
	seedDeliveryEvent(t, db, "orders", `{"id":"A"}`)
	seedDeliveryEvent(t, db, "orders", `{"id":"B"}`)
	src := openDeliverySource(t, db, "unordered", time.Minute)
	claimed, err := src.Claim(t.Context(), 2)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim=%#v err=%v", claimed, err)
	}
	release := make(chan struct{})
	completed := make(chan int64, 2)
	server := httptest.NewServer(
		http.HandlerFunc(
			func(response http.ResponseWriter, request *http.Request) {
				id, _ := strconv.ParseInt(request.Header.Get("X-Pg-Noty-Event-Id"), 10, 64)
				if id == claimed[0].ID {
					<-release
				}
				response.WriteHeader(http.StatusOK)
			},
		),
	)
	defer server.Close()
	client := localListenerClient(time.Second)
	defer client.CloseIdleConnections()
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			GlobalConcurrency: 2,
			OnResult:          func(_ context.Context, result DispatchResult) { completed <- result.Event.ID },
			Listeners: []ListenerConfig{
				{
					Name: "orders", URL: server.URL, Concurrency: 2, Client: client, Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	for _, event := range claimed {
		if !worker.Dispatch(t.Context(), event) {
			t.Fatal("dispatch refused claimed event")
		}
	}
	select {
	case got := <-completed:
		if got != claimed[1].ID {
			t.Fatalf("A completed before held B; IDs=%d/%d", claimed[0].ID, claimed[1].ID)
		}
	case <-time.After(time.Second):
		t.Fatal("B did not complete while A was held")
	}
	close(release)
	worker.Wait()
}
func TestResponseSnippetIsTruncatedAtWriteTime(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":14}`)
	src := openDeliverySource(t, db, "worker", time.Minute)
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK, Body: strings.Repeat("response", 512)})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: http.DefaultClient, Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	var length int
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT octet_length(response_snippet) FROM "+deliveryTable(t, db, schema.TableDeliveries)+" WHERE event_id=$1",
		event.ID,
	).Scan(&length); err != nil {
		t.Fatal(err)
	}
	if length != maxSnippetBytes {
		t.Fatalf("response snippet bytes=%d, want %d", length, maxSnippetBytes)
	}
}
func TestMaxAttemptsMakesTheLastAttemptThenDead(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":15}`)
	src := openDeliverySource(t, db, "worker", time.Minute)
	fixture := newHTTPFixture(t, HTTPReply{Status: 503}, HTTPReply{Status: 503})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: http.DefaultClient, Policy: Policy{
						MaxAttempts: 2, Backoff: "fixed", InitialInterval: time.Millisecond, MaxInterval: time.Second,
					},
				},
			},
		},
	)
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := worker.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		worker.Wait()
		if attempt == 0 {
			if _, err := db.pool.Exec(
				t.Context(),
				"UPDATE "+deliveryTable(t, db, schema.TableEventQueue)+" SET next_attempt_at=now() WHERE event_id=$1",
				event.ID,
			); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := len(fixture.Requests()); got != 2 {
		t.Fatalf("requests=%d, want exactly max_attempts=2", got)
	}
	if present, status, attempts, _, _ := queueState(t, db, event.ID); !present || status != "dead" || attempts != 2 {
		t.Fatalf("queue present=%t status=%q attempts=%d, want dead/2", present, status, attempts)
	}
	if got := deliveryCount(t, db, event.ID); got != 2 {
		t.Fatalf("delivery rows=%d, want two", got)
	}
}
