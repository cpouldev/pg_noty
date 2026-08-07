package delivery

import (
	"context"
	"errors"
	"sync"
	"time"
)

type listenerState struct {
	config ListenerConfig
	sem    *semaphore
	client HTTPDoer
}

type Worker struct {
	events    EventSource
	reclaimer LeaseReclaimer
	config    WorkerConfig
	global    *semaphore
	listeners map[string]*listenerState
	order     []string
	now       Clock
	jitter    JitterFunc
	detach    func(context.Context) context.Context
	hook      ResultHook
	wg        sync.WaitGroup
	mu        sync.Mutex
	inFlight  map[int64]int
	// activity is this worker's drain state, created on first use so a zero Worker still answers.
	activity     *workerActivity
	activityOnce sync.Once
	pending      map[string][]Event
	held         map[int64]time.Time
}

func NewWorker(events EventSource, reclaimer LeaseReclaimer, config WorkerConfig) *Worker {
	if config.BatchSize <= 0 {
		config.BatchSize = 1
	}
	if config.GlobalConcurrency <= 0 {
		config.GlobalConcurrency = 1
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	detach := config.Detach
	if detach == nil {
		detach = context.WithoutCancel
	}
	jitter := config.Jitter
	if jitter == nil {
		jitter = randomJitter
	}
	w := &Worker{events: events, reclaimer: reclaimer, config: config,
		global: newSemaphore(config.GlobalConcurrency), listeners: make(map[string]*listenerState),
		now: now, jitter: jitter, detach: detach, hook: config.OnResult,
		inFlight: make(map[int64]int), pending: make(map[string][]Event), held: make(map[int64]time.Time)}
	for _, listener := range config.Listeners {
		if listener.Name == "" {
			continue
		}
		if _, exists := w.listeners[listener.Name]; exists {
			continue
		}
		var sem *semaphore
		if listener.Concurrency > 0 {
			sem = newSemaphore(listener.Concurrency)
		}
		state := &listenerState{config: listener, sem: sem}
		state.client = listener.Client
		if state.client == nil && listener.URL != "" {
			state.client = NewListenerClient(listener.Timeout, listener.Concurrency, listener.AllowedDestinations)
		}
		w.listeners[listener.Name] = state
		w.order = append(w.order, listener.Name)
	}
	return w
}

func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if w == nil || w.events == nil {
		return 0, errors.New("delivery worker has no event source")
	}
	if w.draining() {
		return 0, nil
	}
	events := w.takePending()
	var expired bool
	events, expired = w.dropExpiredHeld(events)
	if expired {
		// The local lease bound has expired. Do not run reclaim and claim in
		// this same cycle: the source row is still delivering, and claiming it
		// again would burn a second attempt before another worker can reclaim it.
		return w.dispatchBatch(ctx, events), nil
	}
	if w.reclaimer != nil {
		if _, err := w.reclaimer.ReclaimExpired(ctx); err != nil {
			w.retainEvents(events)
			return 0, err
		}
	}
	if w.global.hasCapacity() {
		claimed, err := w.events.Claim(ctx, w.config.BatchSize)
		if err != nil {
			w.retainEvents(events)
			return 0, err
		}
		events = append(events, claimed...)
	}
	return w.dispatchBatch(ctx, events), nil
}

func (w *Worker) Run(ctx context.Context) error {
	if w == nil {
		return errors.New("delivery worker has no event source")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return w.runLoop(ctx)
}

func (w *Worker) Wait() {
	if w != nil {
		w.wg.Wait()
	}
}

func (w *Worker) InFlight() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	total := 0
	for _, count := range w.inFlight {
		total += count
	}
	return total
}

func (w *Worker) CloseIdleConnections() {
	if w == nil {
		return
	}
	for _, state := range w.listeners {
		if client, ok := state.client.(*ListenerClient); ok {
			client.CloseIdleConnections()
		} else if client, ok := state.client.(interface{ CloseIdleConnections() }); ok {
			client.CloseIdleConnections()
		}
	}
}
