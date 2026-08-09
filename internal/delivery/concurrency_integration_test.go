//go:build integration

package delivery

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/source"
)

type concurrentHTTP struct {
	mu              sync.Mutex
	active, maximum int
	calls           []int64
	activeIDs       map[int64]bool
	duplicates      int
	release         <-chan struct{}
}

func (c *concurrentHTTP) Do(request *http.Request) (*http.Response, error) {
	id, _ := strconv.ParseInt(request.Header.Get("X-Pg-Noty-Event-Id"), 10, 64)
	c.mu.Lock()
	if c.activeIDs == nil {
		c.activeIDs = make(map[int64]bool)
	}
	if c.activeIDs[id] {
		c.duplicates++
	}
	c.activeIDs[id] = true
	c.active++
	if c.active > c.maximum {
		c.maximum = c.active
	}
	c.mu.Unlock()
	if c.release != nil {
		<-c.release
	}
	c.mu.Lock()
	c.active--
	delete(c.activeIDs, id)
	c.calls = append(c.calls, id)
	c.mu.Unlock()
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
}
func (c *concurrentHTTP) max() int { c.mu.Lock(); defer c.mu.Unlock(); return c.maximum }
func TestGlobalAndPerListenerCapsBoundTheObservedRequests(t *testing.T) {
	db := newDeliveryDatabase(t)
	src := openDeliverySource(t, db, "worker", time.Minute)
	for index := 0; index < 4; index++ {
		seedDeliveryEvent(t, db, "slow", `{"id":1}`)
		seedDeliveryEvent(t, db, "fast", `{"id":2}`)
	}
	claimed, err := src.Claim(t.Context(), 8)
	if err != nil || len(claimed) != 8 {
		t.Fatalf("claim=%d err=%v, want eight", len(claimed), err)
	}
	release := make(chan struct{})
	slow, fast := &concurrentHTTP{release: release}, &concurrentHTTP{release: release}
	worker := NewWorker(
		src, src, WorkerConfig{
			GlobalConcurrency: 3, Listeners: []ListenerConfig{
				{Name: "slow", Concurrency: 1, Client: slow, Policy: Policy{MaxAttempts: 1}},
				{Name: "fast", Client: fast, Policy: Policy{MaxAttempts: 1}},
			},
		},
	)
	var blocked []source.Event
	for _, event := range claimed {
		if !worker.Dispatch(t.Context(), event) {
			blocked = append(blocked, event)
		}
	}
	for attempt := 0; attempt < 100 && slow.max()+fast.max() < 3; attempt++ {
		time.Sleep(time.Millisecond)
	}
	if slow.max() != 1 || fast.max() < 2 || fast.max() > 2 || slow.max()+fast.max() != 3 {
		t.Fatalf("observed slow max=%d fast max=%d, want 1/2/3", slow.max(), fast.max())
	}
	close(release)
	worker.Wait()
	worker.retainEvents(blocked)
	for len(slow.calls)+len(fast.calls) < 8 {
		runWorker(t, worker)
	}
	if slow.max() != 1 || fast.max() < 2 || fast.max() > 2 || len(slow.calls)+len(fast.calls) != 8 {
		t.Fatalf("post-release caps slow=%d fast=%d calls=%d", slow.max(), fast.max(), len(slow.calls)+len(fast.calls))
	}
}
func TestTwoWorkersClaimWithoutConcurrentDuplicateAndSettleTheScale(t *testing.T) {
	db := newDeliveryDatabase(t)
	seedDeliveryEvents(t, db, "orders", 2000)
	a, b := openDeliverySource(t, db, "worker-a", time.Minute), openDeliverySource(t, db, "worker-b", time.Minute)
	client := &concurrentHTTP{}
	newWorker := func(src *source.TriggerSource) *Worker {
		return NewWorker(
			src,
			src,
			WorkerConfig{
				BatchSize: 500, GlobalConcurrency: 100,
				Listeners: []ListenerConfig{{Name: "orders", Client: client, Policy: Policy{MaxAttempts: 1}}},
			},
		)
	}
	workers := []*Worker{newWorker(a), newWorker(b)}
	done := make(chan error, 2)
	for _, worker := range workers {
		go func(w *Worker) { _, err := w.RunOnce(t.Context()); done <- err }(worker)
	}
	if err, err2 := <-done, <-done; err != nil || err2 != nil {
		t.Fatalf("concurrent worker errors: %v, %v", err, err2)
	}
	for _, worker := range workers {
		worker.Wait()
	}
	for cycle := 0; cycle < 12; cycle++ {
		for _, worker := range workers {
			if _, err := worker.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		for _, worker := range workers {
			worker.Wait()
		}
	}
	var remaining int
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT count(*) FROM "+deliveryTable(t, db, "event_queue"),
	).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("remaining queue rows=%d after two-worker scale run", remaining)
	}
	if len(client.calls) != 2000 || client.duplicates != 0 {
		t.Fatalf("requests=%d duplicates-in-flight=%d, want 2000/0", len(client.calls), client.duplicates)
	}
}
func seedDeliveryEvents(t *testing.T, db deliveryDB, listener string, count int) {
	t.Helper()
	events, queue := deliveryTable(t, db, "events"), deliveryTable(t, db, "event_queue")
	query := "WITH inserted AS (INSERT INTO " + events + " (listener, table_name, operation, payload, txid, occurred_at) SELECT $1, '\"public\".\"orders\"', 'insert', '{\"id\":1}', pg_current_xact_id(), clock_timestamp() FROM generate_series(1,$2) RETURNING id, occurred_at) INSERT INTO " + queue + " (event_id, occurred_at, listener, status, attempts, next_attempt_at) SELECT id, occurred_at, $1, 'pending', 0, clock_timestamp() FROM inserted"
	if _, err := db.pool.Exec(t.Context(), query, listener, count); err != nil {
		t.Fatal(err)
	}
}
func TestNotificationsDisabledStillDeliverOnThePollInterval(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":8}`)
	src := openDeliverySource(t, db, "poller", time.Minute)
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			PollInterval: 20 * time.Millisecond, Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: http.DefaultClient, Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	if err := worker.Run(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	worker.Wait()
	if got := len(fixture.Requests()); got != 1 {
		t.Fatalf("poll-only requests=%d, want one", got)
	}
	if present, status, _, _, _ := queueState(t, db, event.ID); present || status != "" {
		t.Fatalf("poll-only queue present=%t status=%q, want delivered", present, status)
	}
}
func TestLostListenerBackendCostsLatencyNotCorrectness(t *testing.T) {
	db := newDeliveryDatabase(t)
	parsed, err := url.Parse(db.cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	app := "delivery_loss_" + strconv.Itoa(os.Getpid())
	query := parsed.Query()
	query.Set("application_name", app)
	parsed.RawQuery = query.Encode()
	db.cfg.Database.ListenURL = parsed.String()
	src, err := source.Open(
		t.Context(),
		db.pool,
		db.cfg,
		source.Options{LeasedBy: "listener", Lease: time.Minute, Listen: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	var pid int
	for attempt := 0; attempt < 40; attempt++ {
		if err := db.pool.QueryRow(
			t.Context(),
			"SELECT pid FROM pg_stat_activity WHERE application_name=$1",
			app,
		).Scan(&pid); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("listener backend did not start")
	}
	var terminated bool
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT pg_terminate_backend($1)",
		pid,
	).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("terminate listener pid=%d: %v terminated=%t", pid, err, terminated)
	}
	event := seedDeliveryEvent(t, db, "orders", `{"id":9}`)
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			PollInterval: 20 * time.Millisecond, Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: http.DefaultClient, Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_ = worker.Run(ctx)
	worker.Wait()
	if present, status, _, _, _ := queueState(t, db, event.ID); present || status != "" {
		t.Fatalf("after listener loss queue present=%t status=%q, want delivered", present, status)
	}
}
