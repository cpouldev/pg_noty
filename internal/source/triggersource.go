package source

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options configures the trigger-backed source. The delivery worker owns lease policy; these
// fields remain here because the port's adapter is the lifecycle seam it will construct.
type Options struct {
	LeasedBy string
	Lease    time.Duration
	Logger   *slog.Logger
	Listen   bool
}

// TriggerSource is the PostgreSQL implementation of EventSource. Its pool is borrowed and never
// closed here; only the dedicated listening connection and wake-up channel belong to the source.
type TriggerSource struct {
	pool         *pgxpool.Pool
	cfg          config.Config
	opts         Options
	wake         chan struct{}
	now          func() time.Time
	cancel       context.CancelFunc
	listenerDone chan struct{}
	closeDone    chan struct{}
	mu           sync.Mutex
	closed       bool
}

// Open constructs a source without borrowing a pool connection for LISTEN. When listening is
// enabled, the dedicated reader starts asynchronously and reconnects if its backend disappears.
func Open(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, opts Options) (*TriggerSource, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	channel, err := listenChannelName(cfg.Instance)
	if err != nil {
		return nil, err
	}
	listenerContext, cancel := context.WithCancel(ctx)
	source := &TriggerSource{
		pool: pool, cfg: cfg, opts: opts, wake: make(chan struct{}, 1), now: time.Now,
		cancel: cancel, listenerDone: make(chan struct{}), closeDone: make(chan struct{}),
	}
	if opts.Listen {
		go source.listenLoop(listenerContext, channel)
	} else {
		close(source.listenerDone)
	}
	return source, nil
}

// Notify returns the coalescing wake-up channel. It is closed only after Close has stopped LISTEN.
func (s *TriggerSource) Notify() <-chan struct{} { return s.wake }

// Close stops the listener, waits for its goroutine, then closes the sole wake-up channel.
func (s *TriggerSource) Close() error {
	s.mu.Lock()
	if s.closed {
		done := s.closeDone
		s.mu.Unlock()
		<-done
		return nil
	}
	s.closed = true
	s.cancel()
	listenerDone := s.listenerDone
	closeDone := s.closeDone
	s.mu.Unlock()

	<-listenerDone
	close(s.wake)
	close(closeDone)
	return nil
}

func (s *TriggerSource) signalWake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
