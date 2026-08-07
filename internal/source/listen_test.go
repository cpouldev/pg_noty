package source

import (
	"testing"
	"time"
)

func TestWakeSignalIsCapOneAndNonBlocking(t *testing.T) {
	source := &TriggerSource{wake: make(chan struct{}, 1)}
	for range 3 {
		source.signalWake()
	}
	select {
	case <-source.wake:
	default:
		t.Fatal("coalescing signal did not arrive")
	}
	select {
	case <-source.wake:
		t.Fatal("coalescing signal accumulated more than one wake-up")
	default:
	}
}

func TestReconnectBackoffGrowsAndCaps(t *testing.T) {
	backoff := listenInitialBackoff
	previous := time.Duration(0)
	for index := 0; index < 12; index++ {
		if backoff < previous || backoff > listenMaximumBackoff {
			t.Fatalf("backoff %s is not monotonic and capped", backoff)
		}
		previous = backoff
		backoff = nextListenBackoff(backoff)
	}
	if previous != listenMaximumBackoff {
		t.Fatalf("backoff stopped at %s, want cap %s", previous, listenMaximumBackoff)
	}
}
