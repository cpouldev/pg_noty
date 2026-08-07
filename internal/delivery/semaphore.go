package delivery

// semaphore is a weight-one, non-blocking channel semaphore.  A channel keeps
// admission local to this package and avoids promoting golang.org/x/sync into a
// direct dependency merely for TryAcquire(1).
type semaphore struct {
	slots chan struct{}
}

func newSemaphore(capacity int) *semaphore {
	if capacity <= 0 {
		capacity = 1
	}
	return &semaphore{slots: make(chan struct{}, capacity)}
}

func (s *semaphore) tryAcquire() bool {
	if s == nil {
		return true
	}
	select {
	case s.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *semaphore) release() {
	if s == nil {
		return
	}
	select {
	case <-s.slots:
	default:
		// A double release is ignored rather than blocking a worker forever.
	}
}

func (s *semaphore) hasCapacity() bool {
	return s == nil || len(s.slots) < cap(s.slots)
}

// Semaphore is an exported testable wrapper for callers that need a local
// admission primitive without importing an additional module.
type Semaphore struct {
	inner *semaphore
}

// NewSemaphore constructs a bounded non-blocking semaphore.
func NewSemaphore(capacity int) *Semaphore {
	return &Semaphore{inner: newSemaphore(capacity)}
}

// TryAcquire reports whether one slot was acquired immediately.
func (s *Semaphore) TryAcquire() bool {
	return s != nil && s.inner.tryAcquire()
}

// Release returns one acquired slot.
func (s *Semaphore) Release() {
	if s != nil {
		s.inner.release()
	}
}
