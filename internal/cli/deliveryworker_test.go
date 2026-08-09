package cli

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/delivery"
	"github.com/cpouldev/pg_noty/internal/source"
)

// alwaysFailingSource makes Worker.Run return on its first poll, the way a connection reset or a
// failover does, and counts how many times the supervisor started it.
type alwaysFailingSource struct{ claims atomic.Int64 }

func (s *alwaysFailingSource) Claim(context.Context, int) ([]source.Event, error) {
	s.claims.Add(1)
	return nil, errors.New("connection reset by peer")
}
func (s *alwaysFailingSource) Ack(context.Context, source.Event, source.Delivery) error { return nil }
func (s *alwaysFailingSource) Dead(context.Context, source.Event, source.Delivery, string) error {
	return nil
}
func (s *alwaysFailingSource) Nack(context.Context, source.Event, source.Delivery, time.Time) error {
	return nil
}
func (s *alwaysFailingSource) Notify() <-chan struct{} { return nil }
func (s *alwaysFailingSource) Close() error            { return nil }

// TestTheDeliverySupervisorRestartsAfterATransientFailure drives runDeliveryWorker itself rather
// than a second spelling of its loop.
//
// Worker.Run returns on the first error from Claim or ReclaimExpired, and the supervisor used to
// log that and return -- leaving the process up, serving /readyz 200 from the pool, the schema
// version and the startup reconcile, none of which observes the worker, while nothing was
// delivered until someone restarted the pod.
func TestTheDeliverySupervisorRestartsAfterATransientFailure(t *testing.T) {
	failing := &alwaysFailingSource{}
	worker := delivery.NewWorker(
		failing, nil, delivery.WorkerConfig{
			BatchSize: 1, GlobalConcurrency: 1, PollInterval: time.Hour,
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() { defer close(done); runDeliveryWorker(ctx, worker, nil) }()

	// The first backoff is one second, so a second claim can only come from a restart. Two is the
	// assertion: one claim is exactly what the unsupervised code produced.
	deadline := time.After(10 * time.Second)
	for failing.claims.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf(
				"Claim was called %d time(s) in 10s; one transient failure ended delivery "+
					"permanently", failing.claims.Load(),
			)
		case <-time.After(25 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the supervisor did not return when its context was cancelled")
	}
}

// TestTheRestartBackoffGrowsAndIsBounded holds both directions: it must grow, or a database that is
// down becomes a hot loop, and it must stop growing, or a brief failover costs minutes.
func TestTheRestartBackoffGrowsAndIsBounded(t *testing.T) {
	if first, second := deliveryRestartBackoff(0), deliveryRestartBackoff(1); second <= first {
		t.Errorf("backoff did not grow: %s then %s", first, second)
	}
	if got := deliveryRestartBackoff(0); got != time.Second {
		t.Errorf("first backoff = %s, want 1s", got)
	}
	// 1s << 5 is 32s, which the 30s ceiling clamps; every later attempt holds there.
	for _, attempt := range []int{5, 6, 50} {
		if got := deliveryRestartBackoff(attempt); got != 30*time.Second {
			t.Errorf("backoff at attempt %d = %s, want the 30s ceiling", attempt, got)
		}
	}
}

// TestAWorkerIdentityDistinguishesProcessesAndNamesItsInstance holds both halves. The identity must
// differ between processes, or the lease guard in Ack/Nack/Dead compares a string against itself and
// a reclaimed worker can settle the row its successor holds. It must also still name the instance,
// because that is the first question an operator reading noty.event_queue asks.
func TestAWorkerIdentityDistinguishesProcessesAndNamesItsInstance(t *testing.T) {
	identity := workerIdentity("noty")

	if !strings.HasPrefix(identity, "noty/") {
		t.Errorf(
			"identity %q does not begin with its instance; an operator reading leased_by cannot "+
				"tell which deployment holds the row", identity,
		)
	}
	if !strings.HasSuffix(identity, "/"+strconv.Itoa(os.Getpid())) {
		t.Errorf(
			"identity %q does not end in this process's pid, so two processes of one deployment "+
				"on one host would share it", identity,
		)
	}
	// The defect this replaces: the identity was the instance alone, so it was constant across every
	// process of a deployment.
	if identity == "noty" {
		t.Fatal("identity is the bare instance, which every replica shares")
	}
	if second := workerIdentity("noty"); second != identity {
		t.Errorf(
			"identity is not stable within one process: %q then %q; a lease taken under one "+
				"spelling could not be settled under the other", identity, second,
		)
	}
	if other := workerIdentity("other"); other == identity {
		t.Errorf("two instances produced the same identity %q", other)
	}
}
