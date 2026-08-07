package delivery

import (
	"context"
	"errors"
	"github.com/cpouldev/pg_noty/internal/source"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWorkerReclaimPrecedesClaim(t *testing.T) {
	trace := make([]string, 0, 2)
	src := &workerSource{
		claim:   func() []Event { trace = append(trace, "claim"); return nil },
		reclaim: func() { trace = append(trace, "reclaim") },
	}
	worker := NewWorker(src, src, WorkerConfig{BatchSize: 1})
	runWorker(t, worker)
	if strings.Join(trace, ",") != "reclaim,claim" {
		t.Fatalf("trace %v, want reclaim before claim", trace)
	}
}
func TestWorkerPayloadNPlusOneDeadBeforeHTTPAdmission(t *testing.T) {
	src := &workerSource{}
	client := &workerHTTP{}
	worker := NewWorker(src, nil, WorkerConfig{Listeners: []ListenerConfig{testListener("orders", 0, 4, client)}})
	payload := []byte(`12345`)
	event := testEvent(9, "orders", payload)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if worker.Dispatch(ctx, event) {
		t.Fatal("oversized payload acquired HTTP admission")
	}
	if client.calls != 0 {
		t.Fatalf("HTTP calls = %d, want zero", client.calls)
	}
	if len(src.dead) != 1 || !strings.Contains(src.dead[0], "5") || !strings.Contains(src.dead[0], "4") {
		t.Fatalf("dead records = %#v, want measured size and limit", src.dead)
	}
	if string(event.Payload) != string(payload) {
		t.Fatal("oversized payload was mutated")
	}
	if src.deadCtxErr != nil {
		t.Fatalf("dead context = %v, want detached", src.deadCtxErr)
	}
}
func TestWorkerSwallowsLateClaimSentinel(t *testing.T) {
	src := &workerSource{ackErr: source.ErrEventNotClaimed}
	worker := NewWorker(
		src,
		nil,
		WorkerConfig{Listeners: []ListenerConfig{testListener("orders", 0, 1024, &workerHTTP{response: okResponse()})}},
	)
	if !worker.Dispatch(context.Background(), testEvent(4, "orders", []byte(`{}`))) {
		t.Fatal("late-claim event was not admitted")
	}
	worker.Wait()
	if src.ackCalls != 1 || len(src.dead) != 0 || src.nackCalls != 0 {
		t.Fatalf("source calls ack=%d nack=%d dead=%d", src.ackCalls, src.nackCalls, len(src.dead))
	}
}
func TestWorkerReadErrorDoesNotAckSuccessfulStatus(t *testing.T) {
	src := &workerSource{}
	client := &workerHTTP{
		response: &http.Response{
			StatusCode: 200, Body: &failingRemainderBody{readErr: errors.New("body failed")},
		},
	}
	cfg := testListener("orders", 0, 1024, client)
	cfg.Policy = Policy{MaxAttempts: 2, InitialInterval: time.Second, MaxInterval: time.Second}
	worker := NewWorker(src, nil, WorkerConfig{Listeners: []ListenerConfig{cfg}})
	event := testEvent(5, "orders", []byte(`{}`))
	if !worker.Dispatch(context.Background(), event) {
		t.Fatal("request was not admitted")
	}
	worker.Wait()
	if src.ackCalls != 0 || src.nackCalls != 1 {
		t.Fatalf("ack=%d nack=%d, want retry without ack", src.ackCalls, src.nackCalls)
	}
}
func TestWorkerInFlightCountsDuplicateIDs(t *testing.T) {
	started, release := make(chan struct{}, 2), make(chan struct{})
	client := &workerHTTP{factory: okResponse, started: started, block: release}
	worker := NewWorker(
		nil,
		nil,
		WorkerConfig{GlobalConcurrency: 2, Listeners: []ListenerConfig{testListener("orders", 2, 1024, client)}},
	)
	event := testEvent(7, "orders", []byte(`{}`))
	if !worker.Dispatch(context.Background(), event) || !worker.Dispatch(context.Background(), event) {
		t.Fatal("duplicate admissions failed")
	}
	<-started
	<-started
	if got := worker.InFlight(); got != 2 {
		t.Fatalf("in-flight=%d, want 2 for duplicate ids", got)
	}
	close(release)
	worker.Wait()
	if got := worker.InFlight(); got != 0 {
		t.Fatalf("in-flight after completion=%d", got)
	}
}
func TestWorkerRetainsCappedBatchAndAdvancesUncapped(t *testing.T) {
	slowClient := &workerHTTP{response: okResponse()}
	fastClient := &workerHTTP{response: okResponse()}
	batch := []Event{
		testEvent(1, "slow", []byte(`{}`)), testEvent(2, "fast", []byte(`{}`)),
	}
	src := &workerSource{batch: batch}
	worker := NewWorker(
		src,
		nil,
		WorkerConfig{
			BatchSize: 2, GlobalConcurrency: 2, Listeners: []ListenerConfig{
				testListener("slow", 1, 1024, slowClient), testListener("fast", 0, 1024, fastClient),
			},
		},
	)
	if !worker.listeners["slow"].sem.tryAcquire() {
		t.Fatal("failed to saturate slow listener")
	}
	runWorker(t, worker)
	if slowClient.calls != 0 || fastClient.calls != 1 || src.nackCalls != 0 {
		t.Fatalf("first cycle calls slow=%d fast=%d nack=%d", slowClient.calls, fastClient.calls, src.nackCalls)
	}
	if len(worker.pending["slow"]) != 1 {
		t.Fatalf("retained slow queue = %#v, want one event", worker.pending["slow"])
	}
	runWorker(t, worker)
	if slowClient.calls != 0 {
		t.Fatalf("capped event dispatched while cap remained full: %d", slowClient.calls)
	}
	worker.listeners["slow"].sem.release()
	runWorker(t, worker)
	if slowClient.calls != 1 || fastClient.calls != 1 || src.nackCalls != 0 {
		t.Fatalf(
			"final calls slow=%d fast=%d nack=%d, want one each and no nack",
			slowClient.calls,
			fastClient.calls,
			src.nackCalls,
		)
	}
}
func okResponse() *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}
}
func testEvent(id int64, listener string, payload []byte) Event {
	return Event{ID: id, Listener: listener, Table: `"public"."t"`, Payload: payload}
}
func testListener(name string, concurrency, max int, client HTTPDoer) ListenerConfig {
	return ListenerConfig{
		Name: name, URL: "http://" + name, Concurrency: concurrency, MaxPayloadBytes: max, Client: client,
	}
}
func runWorker(t *testing.T, worker *Worker) {
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
}

type workerHTTP struct {
	mu       sync.Mutex
	calls    int
	response *http.Response
	factory  func() *http.Response
	started  chan<- struct{}
	block    <-chan struct{}
}

func (c *workerHTTP) Do(*http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if c.started != nil {
		c.started <- struct{}{}
	}
	if c.block != nil {
		<-c.block
	}
	if c.factory != nil {
		return c.factory(), nil
	}
	return c.response, nil
}

type workerSource struct {
	claim               func() []Event
	reclaim             func()
	batch               []Event
	ackErr              error
	ackCalls, nackCalls int
	dead                []string
	deadCtxErr          error
}

func (s *workerSource) ReclaimExpired(context.Context) (int64, error) {
	if s.reclaim != nil {
		s.reclaim()
	}
	return 0, nil
}
func (s *workerSource) Claim(context.Context, int) ([]Event, error) {
	if s.claim != nil {
		return s.claim(), nil
	}
	if s.batch == nil {
		return nil, nil
	}
	batch := s.batch
	s.batch = nil
	return batch, nil
}
func (s *workerSource) Ack(context.Context, Event, Delivery) error { s.ackCalls++; return s.ackErr }
func (s *workerSource) Nack(context.Context, Event, Delivery, time.Time) error {
	s.nackCalls++
	return nil
}
func (s *workerSource) Dead(ctx context.Context, _ Event, _ Delivery, reason string) error {
	s.deadCtxErr = ctx.Err()
	s.dead = append(s.dead, reason)
	return nil
}
func (s *workerSource) Notify() <-chan struct{} { return nil }
func (s *workerSource) Close() error            { return nil }
