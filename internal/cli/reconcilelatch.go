package cli

import "sync"

type latchState uint8

const (
	latchPending latchState = iota
	latchDischarged
	latchFailed
)

type reconcileLatch struct {
	mu    sync.RWMutex
	state latchState
}

func (latch *reconcileLatch) resolve(err error) {
	latch.mu.Lock()
	if err == nil {
		latch.state = latchDischarged
	} else {
		latch.state = latchFailed
	}
	latch.mu.Unlock()
}

func (latch *reconcileLatch) ready() bool {
	latch.mu.RLock()
	defer latch.mu.RUnlock()
	return latch.state == latchDischarged
}
