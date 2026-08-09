package delivery

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"
)

type reconnectFunc func(context.Context) (<-chan struct{}, error)
type wakeMachine struct {
	interval     time.Duration
	jitter       JitterFunc
	hinted       bool
	listening    bool
	reconnecting bool
}

func newWakeMachine(interval time.Duration, jitter JitterFunc) *wakeMachine {
	if interval <= 0 {
		interval = time.Second
	}
	return &wakeMachine{interval: interval, jitter: jitter, listening: true}
}
func (m *wakeMachine) poll() wakeAction { return wakeAction{poll: true} }
func (m *wakeMachine) notify(open bool) wakeAction {
	if !open {
		m.listening = false
		return wakeAction{}
	}
	m.listening = true
	if m.hinted {
		return wakeAction{}
	}
	m.hinted = true
	return wakeAction{hint: true, delay: wakeDelay(m.interval, m.jitter)}
}
func (m *wakeMachine) fired() wakeAction {
	m.hinted = false
	return m.poll()
}
func (m *wakeMachine) lost() bool {
	m.listening = false
	if m.reconnecting {
		return false
	}
	m.reconnecting = true
	return true
}
func (m *wakeMachine) reconnected(open bool) {
	m.reconnecting = false
	m.listening = open
}

type wakeAction struct {
	hint  bool
	poll  bool
	delay time.Duration
}

func wakeDelay(interval time.Duration, draw JitterFunc) time.Duration {
	if interval <= 0 {
		return 0
	}
	limit := int64(interval)
	bound := limit
	if limit < math.MaxInt64 {
		bound++
	}
	if draw == nil {
		draw = func(n int64) int64 {
			if n <= 1 {
				return 0
			}
			return rand.Int63n(n)
		}
	}
	delay := draw(bound)
	if delay < 0 {
		return 0
	}
	if delay > limit {
		return interval
	}
	return time.Duration(delay)
}

type wakeLoop struct {
	machine           *wakeMachine
	notify            <-chan struct{}
	reconnect         reconnectFunc
	reconnectInterval time.Duration
}

func newWakeLoop(interval time.Duration, notify <-chan struct{}, jitter JitterFunc, reconnect reconnectFunc) wakeLoop {
	if interval <= 0 {
		interval = time.Second
	}
	return wakeLoop{newWakeMachine(interval, jitter), notify, reconnect, interval}
}

func (l wakeLoop) run(ctx context.Context, poll func(context.Context) error, stop func(context.Context)) error {
	if poll == nil {
		return errors.New("delivery wake loop has no poll callback")
	}
	if err := poll(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(l.machine.interval)
	defer ticker.Stop()
	notify := l.notify
	var hint <-chan time.Time
	var timer *time.Timer
	var reconnectResult <-chan (<-chan struct{})
	var retry <-chan time.Time
	var retryTimer *time.Timer
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			if stop != nil {
				stop(ctx)
			}
			return ctx.Err()
		case <-ticker.C:
			if err := poll(ctx); err != nil {
				return err
			}
		case _, open := <-notify:
			if !open {
				notify = nil
				reconnectResult = l.reconnectIfNeeded(ctx)
				continue
			}
			action := l.machine.notify(true)
			if action.hint && hint == nil {
				timer = time.NewTimer(action.delay)
				hint = timer.C
			}
		case <-hint:
			l.machine.fired()
			hint, timer = nil, nil
			if err := poll(ctx); err != nil {
				return err
			}
		case channel := <-reconnectResult:
			reconnectResult = nil
			l.machine.reconnected(channel != nil)
			if channel != nil {
				notify = channel
			} else {
				retryTimer = time.NewTimer(l.reconnectInterval)
				retry = retryTimer.C
			}
		case <-retry:
			retry, retryTimer = nil, nil
			if l.machine.lost() && l.reconnect != nil {
				reconnectResult = l.startReconnect(ctx)
			}
		}
	}
}

func (l wakeLoop) reconnectIfNeeded(ctx context.Context) <-chan (<-chan struct{}) {
	if l.machine.lost() && l.reconnect != nil {
		return l.startReconnect(ctx)
	}
	return nil
}

func (l wakeLoop) startReconnect(ctx context.Context) <-chan (<-chan struct{}) {
	result := make(chan (<-chan struct{}), 1)
	go func() {
		channel, err := l.reconnect(ctx)
		if err != nil {
			channel = nil
		}
		result <- channel
	}()
	return result
}

func (w *Worker) runLoop(ctx context.Context) error {
	var notify <-chan struct{}
	if w.events != nil {
		notify = w.events.Notify()
	}
	loop := newWakeLoop(w.pollInterval(), notify, w.jitter, nil)
	return loop.run(ctx, func(runCtx context.Context) error {
		_, err := w.RunOnce(runCtx)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}, func(stopCtx context.Context) { w.Drain(stopCtx, w.config.DrainTimeout) })
}
