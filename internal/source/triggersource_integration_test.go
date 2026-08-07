//go:build integration

package source

import (
	"testing"
	"time"
)

func TestCloseWaitsForListenerAndLeavesChannelOpenUntilClose(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	cfg := harnessConfig(t)
	source, err := Open(t.Context(), pool, cfg, Options{Listen: true})
	if err != nil {
		t.Fatal(err)
	}
	waitForDedicatedListener(t, pool, source)
	select {
	case _, open := <-source.Notify():
		if !open {
			t.Fatal("listener loss closed the wake-up channel")
		}
	default:
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, open := <-source.Notify():
		if open {
			t.Fatal("Close returned with an open wake-up channel")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not close the wake-up channel")
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestCloseBoundaryReturnsSourceClosed asserts only that Close records the state the boundary is
// read from. What the six port methods answer once it is recorded is asserted behaviourally by
// closeboundary_integration_test.go's TestEveryCallIssuedAfterCloseIsRefusedWithSourceClosed, and a
// call already running when Close is invoked by inflightclose_integration_test.go's
// TestACallInFlightWhenCloseIsCalledStillCompletes.
func TestCloseBoundaryReturnsSourceClosed(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	source, err := Open(t.Context(), pool, harnessConfig(t), Options{Listen: false})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if !isSourceClosed(source) {
		t.Fatal("source did not record its closed state")
	}
}

func isSourceClosed(source *TriggerSource) bool {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.closed
}
