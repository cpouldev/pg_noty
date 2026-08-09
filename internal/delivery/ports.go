package delivery

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/cpouldev/pg_noty/internal/source"
)

// EventSource is the frozen six-method transition port.  It is an alias rather
// than a redeclaration so a source backed by something other than PostgreSQL can
// be substituted without changing the worker.
type EventSource = source.EventSource

// LeaseReclaimer is the sibling liveness port.  Reclaim is intentionally not a
// seventh EventSource method: the queue adapter owns the sweep and the worker
// only decides when to invoke it.
type LeaseReclaimer = source.LeaseReclaimer

type Event = source.Event
type Delivery = source.Delivery

// Clock and Jitter are dependency seams used by policy and worker tests.  A nil
// Clock is replaced by time.Now; a nil Jitter makes policy deterministic.
type Clock func() time.Time

// HTTPDoer is the smallest HTTP boundary needed by the worker.  ListenerClient
// and *http.Client both satisfy it, while tests can provide a named fake.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// ListenerConfig is the delivery half of one resolved listener.  It deliberately
// contains no database or queue concepts.  MaxPayloadBytes is compared with the
// immutable source payload before a request consumes an admission slot.
type ListenerConfig struct {
	Name            string
	URL             string
	Method          string
	Headers         map[string]string
	Signer          Signer
	Policy          Policy
	Timeout         time.Duration
	Concurrency     int
	MaxPayloadBytes int
	Client          HTTPDoer
	// AllowedDestinations carries worker.allowed_destination_cidrs to this listener's dialer.
	AllowedDestinations []*net.IPNet
}

// WorkerConfig controls admission and the claim loop.  BatchSize and
// GlobalConcurrency are normalised to one when non-positive, matching the
// fail-closed behaviour expected of a fully resolved configuration.
type WorkerConfig struct {
	BatchSize         int
	GlobalConcurrency int
	PollInterval      time.Duration
	// LeaseTimeout bounds how long a claimed event may remain held locally.
	LeaseTimeout time.Duration
	DrainTimeout time.Duration
	Listeners    []ListenerConfig
	Now          Clock
	Jitter       JitterFunc
	Detach       func(context.Context) context.Context
	OnResult     ResultHook
}

// DispatchResult is the immutable observation handed to an optional result
// hook after a request has completed.  The worker still records the default
// outcome through EventSource; the hook is useful for metrics and test traces.
type DispatchResult struct {
	Event    Event
	Listener ListenerConfig
	Request  *http.Request
	Status   int
	Outcome  Outcome
	Snippet  []byte
	Err      error
	Started  time.Time
	Finished time.Time
}

// ResultHook observes an attempt after its source transition has been chosen.
// It must not perform queue SQL; EventSource remains the only transition port.
type ResultHook func(context.Context, DispatchResult)

type dispatchState uint8

const dispatchBlocked dispatchState = iota
const dispatchConsumed dispatchState = 1
const dispatchAccepted dispatchState = 2

func buildRequest(ctx context.Context, listener ListenerConfig, event Event, body []byte, now time.Time) (
	*http.Request,
	error,
) {
	method := listener.Method
	if method == "" {
		method = http.MethodPost
	}
	request, err := http.NewRequestWithContext(ctx, method, listener.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for name, value := range listener.Headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("X-Pg-Noty-Event-Id", strconv.FormatInt(event.ID, 10))
	request.Header.Set("X-Pg-Noty-Timestamp", strconv.FormatInt(now.Unix(), 10))
	request.Header.Set("X-Pg-Noty-Attempt", strconv.Itoa(event.Attempt))
	if signed := listener.Signer.Header(now.Unix(), body); signed != "" {
		request.Header.Set("X-Pg-Noty-Signature", signed)
	}
	return request, nil
}

func doRequest(client HTTPDoer, request *http.Request) (*http.Response, []byte, error) {
	if client == nil {
		return nil, nil, errors.New("listener has no HTTP client")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	snippet, readErr := DrainResponseBody(response, maxSnippetBytes)
	if readErr != nil {
		return response, snippet, readErr
	}
	return response, snippet, nil
}

func responseStatus(response *http.Response) int {
	if response == nil {
		return 0
	}
	return response.StatusCode
}

func deliveryError(delivery Delivery) error {
	if delivery.Err == "" {
		return nil
	}
	return errors.New(delivery.Err)
}

func swallowLateClaim(err error) error {
	if errors.Is(err, source.ErrEventNotClaimed) {
		return nil
	}
	return err
}

func lateClaim(err error) bool { return errors.Is(err, source.ErrEventNotClaimed) }

func allQueuesEmpty(queues map[string][]Event) bool {
	for _, events := range queues {
		if len(events) > 0 {
			return false
		}
	}
	return true
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (w *Worker) consumeUnknown(ctx context.Context, event Event) {
	reason := "unknown listener " + event.Listener
	err := w.settleDead(w.detach(ctx), event, Delivery{Err: reason}, reason)
	w.observeSettlement(ctx, event, ListenerConfig{}, err)
}

// listenerNames is the dispatch order for one batch: configured listeners first, then any listener
// a claimed event names that is not configured. The unknown ones must be here -- dispatchBatch
// visits only these names, and its state == nil arm is what dead-letters an event whose listener was
// disabled while its queue rows survived.
func (w *Worker) listenerNames(queues map[string][]Event) []string {
	order := append([]string(nil), w.order...)
	unknown := make([]string, 0, len(queues))
	for name := range queues {
		if !contains(order, name) {
			unknown = append(unknown, name)
		}
	}
	// Sorted: ranging a map is unordered and these names now reach dispatch.
	slices.Sort(unknown)
	return append(order, unknown...)
}
