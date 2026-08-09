package delivery

import (
	"strings"
	"testing"
)

// TestAnEventForAnUnknownListenerIsDeadLetteredRatherThanHeldForever reaches the branch
// dispatchBatch already has for a listener it cannot dispatch to. listenerNames added a queue's name
// to the dispatch order only when the listener was *known*, and every known name is already in
// w.order -- so the condition could never be true, the state == nil arm was dead code, and an event
// whose listener is not configured was never visited at all.
//
// It is reachable in ordinary operation: disabling a listener drops it from workerConfig while its
// queue rows survive, and Claim does not filter by listener. Those rows were retained in memory,
// expired against their lease, were reclaimed server-side and re-claimed forever -- incrementing
// attempts each cycle and never reaching dead.
func TestAnEventForAnUnknownListenerIsDeadLetteredRatherThanHeldForever(t *testing.T) {
	client := &workerHTTP{response: okResponse()}
	src := &workerSource{batch: []Event{
		testEvent(1, "was_disabled", []byte(`{}`)),
		testEvent(2, "still_here", []byte(`{}`)),
	}}
	worker := NewWorker(src, nil, WorkerConfig{BatchSize: 2, GlobalConcurrency: 2,
		Listeners: []ListenerConfig{testListener("still_here", 0, 1024, client)}})

	runWorker(t, worker)

	if len(src.dead) != 1 || !strings.Contains(src.dead[0], "was_disabled") {
		t.Fatalf("dead reasons = %v, want one naming the unknown listener; the event is otherwise "+
			"retained in memory and re-claimed forever, burning an attempt each cycle", src.dead)
	}
	if client.calls != 1 {
		t.Errorf("configured listener received %d requests, want 1; an unknown listener must not "+
			"starve the listeners that are configured", client.calls)
	}
	if held := len(worker.pending["was_disabled"]); held != 0 {
		t.Errorf("%d events for the unknown listener are still held", held)
	}
}
