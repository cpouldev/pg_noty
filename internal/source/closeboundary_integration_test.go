//go:build integration

package source

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The after-Close half of the lifecycle boundary clause, which nothing reached: ErrSourceClosed was
// declared, documented and asserted against itself in errors_test.go, and no test had ever called a
// method on a closed source -- so ensureUsable's closed branch could return nil, or be deleted, with
// the whole suite green. The in-flight half is inflightclose_integration_test.go's
// TestACallInFlightWhenCloseIsCalledStillCompletes.
//
// Both sides of the guard are driven from one call list, so a source that answered ErrSourceClosed
// whatever its state fails the open pass rather than passing the closed one. And the refusal is
// asserted to be a refusal rather than a report issued after the work: the claimed row the refused
// calls named, and a pending row a refused Claim would have taken, are both read back afterwards.

// portCall is one call on the port. It answers with the events the call yielded -- nil for the three
// transitions -- so Claim's promise to hand out none when closed is asserted through the same list
// rather than beside it.
type portCall func(t *testing.T, source *TriggerSource, target Event) ([]Event, error)

// theBoundaryDelivery is the attempt each transition would record if it ran, which is what makes the
// delivery count below a measurement of whether the refusal came first.
var theBoundaryDelivery = Delivery{HTTPStatus: 200, Duration: time.Millisecond}

// callsAnsweringWithAnError is every EventSource method whose contract is an error value, keyed by
// the method's own name so TestEveryPortMethodHasAnAnswerAfterClose can reconcile the set against
// the port rather than against a copy of it. Notify and Close answer differently and are named in
// answeredWithoutAnError.
var callsAnsweringWithAnError = map[string]portCall{
	"Claim": func(t *testing.T, source *TriggerSource, _ Event) ([]Event, error) {
		return source.Claim(t.Context(), 1)
	},
	"Ack": func(t *testing.T, source *TriggerSource, target Event) ([]Event, error) {
		return nil, source.Ack(t.Context(), target, theBoundaryDelivery)
	},
	"Nack": func(t *testing.T, source *TriggerSource, target Event) ([]Event, error) {
		return nil, source.Nack(t.Context(), target, theBoundaryDelivery, source.now().Add(theRetryDelay))
	},
	"Dead": func(t *testing.T, source *TriggerSource, target Event) ([]Event, error) {
		return nil, source.Dead(t.Context(), target, theBoundaryDelivery, theDeadReason)
	},
}

func afterCloseCallNames() []string { return slices.Sorted(maps.Keys(callsAnsweringWithAnError)) }

func claimedEvent(t *testing.T, claimed []Event, id int64) Event {
	t.Helper()
	for _, event := range claimed {
		if event.ID == id {
			return event
		}
	}
	t.Fatalf("event %d is not among the %d claimed", id, len(claimed))
	return Event{}
}

func TestEveryCallIssuedAfterCloseIsRefusedWithSourceClosed(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	source := openListeningSource(t, pool, harnessConfig(t), newListenLog())
	exercised, refused := seedQueueEvent(t, pool, "exercised"), seedQueueEvent(t, pool, "refused")
	claimed, err := source.Claim(t.Context(), 2)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim = %#v, %v; want both seeded events claimed before the boundary", claimed, err)
	}

	for _, name := range afterCloseCallNames() {
		call, target := callsAnsweringWithAnError[name], claimedEvent(t, claimed, exercised.ID)
		if _, err := call(t, source, target); errors.Is(err, ErrSourceClosed) {
			t.Fatalf("%s returned ErrSourceClosed on a source that is still open, so the sentinel "+
				"says nothing about the lifecycle", name)
		}
	}

	// Seeded after the open pass so the Claim in it cannot have taken this row, and left pending so
	// that a Claim which ran after Close would visibly take it.
	pending := seedQueueEvent(t, pool, "pending")
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	assertEveryCallIsRefused(t, source, claimedEvent(t, claimed, refused.ID))
	assertNotifyAfterCloseReportsTheSourceFinished(t, source)
	assertASecondCloseReportsNoFailure(t, source)
	assertTheRefusedCallsChangedNothing(t, pool, source, refused.ID, pending.ID)
}

func assertEveryCallIsRefused(t *testing.T, source *TriggerSource, target Event) {
	t.Helper()
	for _, name := range afterCloseCallNames() {
		events, err := callsAnsweringWithAnError[name](t, source, target)
		if !errors.Is(err, ErrSourceClosed) {
			t.Errorf("%s issued after Close returned %v, want ErrSourceClosed: a caller cannot tell a "+
				"finished source from a failing one", name, err)
		}
		if len(events) != 0 {
			t.Errorf("%s issued after Close returned %d events; a closed source hands out none",
				name, len(events))
		}
	}
}

// assertTheRefusedCallsChangedNothing is what separates "refused" from "reported after the fact".
// Ack would have deleted the claimed row, Nack and Dead would have cleared its lease, and Claim
// would have taken the pending one.
func assertTheRefusedCallsChangedNothing(t *testing.T, pool *pgxpool.Pool, source *TriggerSource, claimedID, pendingID int64) {
	t.Helper()
	row := readQueueRow(t, pool, claimedID)
	if !row.present || row.status != "delivering" || row.attempts != 1 {
		t.Fatalf("the refused calls left the claimed row present=%t status=%s attempts=%d, want it "+
			"exactly as they found it", row.present, row.status, row.attempts)
	}
	if row.leasedBy == nil || *row.leasedBy != source.opts.LeasedBy {
		t.Errorf("leased_by = %v after the refused calls, want the lease %q the claim took: a refusal "+
			"that ran the transition first is not a refusal", row.leasedBy, source.opts.LeasedBy)
	}
	if got := deliveryCount(t, pool, claimedID); got != 0 {
		t.Errorf("the refused calls recorded %d delivery attempts, want 0", got)
	}
	if waiting := readQueueRow(t, pool, pendingID); !waiting.present || waiting.status != "pending" || waiting.attempts != 0 {
		t.Errorf("the refused Claim left the pending row present=%t status=%s attempts=%d, want an "+
			"untouched pending row", waiting.present, waiting.status, waiting.attempts)
	}
}
