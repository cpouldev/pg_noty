package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/cli"
	"github.com/getsentry/sentry-go"
)

const testSentryDSN = "https://public@example.com/1"

func TestSentryIsDisabledWithoutDSN(t *testing.T) {
	for _, dsn := range []string{"", " \t\n"} {
		transport := &recordingSentryTransport{}
		runtime, err := newSentryRuntime(dsn, transport)
		if err != nil || runtime != nil {
			t.Fatalf("newSentryRuntime(%q) = (%v, %v), want (nil, nil)", dsn, runtime, err)
		}
		if transport.wasConfigured() {
			t.Fatalf("newSentryRuntime(%q) configured a transport while disabled", dsn)
		}
	}
}

func TestRunReportsCommandFailuresBeforeReturning(t *testing.T) {
	t.Setenv("SENTRY_RELEASE", "pg_noty@test")
	t.Setenv("SENTRY_ENVIRONMENT", "sentry-test")
	for _, testCase := range []struct {
		name       string
		dsn        string
		args       []string
		wantStatus int
		wantEvents int
	}{
		{name: "disabled", args: []string{"sentry-unknown-command"}, wantStatus: 1},
		{name: "help", dsn: testSentryDSN, args: []string{"--help"}},
		{name: "failure", dsn: testSentryDSN, args: []string{"sentry-unknown-command"}, wantStatus: 1, wantEvents: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			transport := &recordingSentryTransport{}
			var output bytes.Buffer
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			status := run(ctx, testCase.args, cli.Streams{Out: &output, Err: &output}, testCase.dsn, transport)
			if status != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; output = %q", status, testCase.wantStatus, output.String())
			}
			events, lifecycle, deadline := transport.snapshot()
			if len(events) != testCase.wantEvents {
				t.Fatalf("captured %d events, want %d", len(events), testCase.wantEvents)
			}
			if testCase.dsn == "" {
				if transport.wasConfigured() || len(lifecycle) != 0 {
					t.Fatal("disabled Sentry used its transport")
				}
				return
			}
			if strings.Join(lifecycle, ",") != "flush,close" || deadline <= 0 || deadline > sentryShutdownTimeout {
				t.Fatalf("shutdown = %v, deadline = %s; want bounded flush before close despite cancellation", lifecycle, deadline)
			}
			for _, event := range events {
				if event.Release != "pg_noty@test" || event.Environment != "sentry-test" {
					t.Fatalf("release = %q, environment = %q; want labels from the environment", event.Release, event.Environment)
				}
				if len(event.Exception) == 0 || !strings.Contains(output.String(), event.Exception[0].Value) {
					t.Fatalf("event = %#v, want the same error printed to stderr %q", event, output.String())
				}
			}
		})
	}
}

func TestSentryRuntimeUsesErrorOnlyPrivateDefaultsAndClosesInOrder(t *testing.T) {
	transport := &recordingSentryTransport{}
	runtime, err := newSentryRuntime(testSentryDSN, transport)
	if err != nil {
		t.Fatal(err)
	}
	if runtime == nil || sentry.GetHubFromContext(runtime.context(t.Context())) != runtime.hub {
		t.Fatal("enabled Sentry runtime did not attach its hub to the command context")
	}
	options := runtime.client.Options()
	if options.EnableTracing || !options.DisableLogs || !options.DisableMetrics || !options.AttachStacktrace {
		t.Fatalf("Sentry options = tracing:%t logs-disabled:%t metrics-disabled:%t stacktraces:%t, want error-only with stacktraces", options.EnableTracing, options.DisableLogs, options.DisableMetrics, options.AttachStacktrace)
	}
	collection := runtime.client.GetDataCollection()
	headers := collection.HTTPHeaders
	if collection.UserInfo.Or(true) || len(collection.HTTPBodies) != 0 ||
		collection.Cookies == nil || collection.Cookies.Mode != sentry.CollectionOff ||
		headers == nil || headers.Request == nil || headers.Response == nil ||
		headers.Request.Mode != sentry.CollectionDenyList || headers.Response.Mode != sentry.CollectionDenyList ||
		collection.QueryParams == nil || collection.QueryParams.Mode != sentry.CollectionDenyList {
		t.Fatalf("Sentry data collection enables private context: %#v", collection)
	}

	runtime.hub.CaptureException(errors.New("sentry event probe"))
	runtime.shutdown()
	events, lifecycle, deadline := transport.snapshot()
	if len(events) != 1 || len(events[0].Exception) == 0 || events[0].Exception[0].Value != "sentry event probe" {
		t.Fatalf("captured events = %#v, want the probe exception", events)
	}
	if strings.Join(lifecycle, ",") != "flush,close" {
		t.Fatalf("transport lifecycle = %v, want flush before close", lifecycle)
	}
	if deadline <= 0 || deadline > sentryShutdownTimeout {
		t.Fatalf("flush deadline = %s, want a fresh deadline within %s", deadline, sentryShutdownTimeout)
	}
}

func TestInvalidSentryDSNStopsBeforeTheCommandWithoutLeakingIt(t *testing.T) {
	const dsn = "secret-marker://public@example.com/1"
	transport := &recordingSentryTransport{}
	var output bytes.Buffer
	status := run(
		t.Context(),
		[]string{"--help"},
		cli.Streams{Out: &output, Err: &output},
		dsn,
		transport,
	)
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if got := output.String(); got != "error: SENTRY_DSN is invalid\n" || strings.Contains(got, "secret-marker") {
		t.Fatalf("output = %q, want a redacted SENTRY_DSN error", got)
	}
	if transport.wasConfigured() {
		t.Fatal("invalid SENTRY_DSN configured a transport")
	}
}

func TestSentryRuntimeReportsAndRethrowsPanics(t *testing.T) {
	transport := &recordingSentryTransport{}
	runtime, err := newSentryRuntime(testSentryDSN, transport)
	if err != nil {
		t.Fatal(err)
	}
	const probe = "panic probe"
	recovered := recoverThroughSentry(runtime, t.Context(), probe)
	if recovered != probe {
		t.Fatalf("recovered panic = %#v, want %q", recovered, probe)
	}
	events, _, _ := transport.snapshot()
	if len(events) != 1 || events[0].Message != probe || len(events[0].Threads) == 0 || events[0].Threads[0].Stacktrace == nil {
		t.Fatalf("captured panic events = %#v, want the panic probe", events)
	}
	runtime.shutdown()
}

func TestSentryShutdownIsBoundedWhenServerStalls(t *testing.T) {
	received := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(received)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(release)
	dsn := "http://public@" + strings.TrimPrefix(server.URL, "http://") + "/1"
	runtime, err := newSentryRuntime(dsn, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.client.Close)
	runtime.hub.CaptureException(errors.New("stalled Sentry probe"))
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("Sentry did not send the probe request")
	}
	done := make(chan struct{})
	go func() {
		runtime.shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(sentryShutdownTimeout + time.Second):
		t.Fatal("Sentry shutdown exceeded its timeout while the server was stalled")
	}
}

func recoverThroughSentry(runtime *sentryRuntime, ctx context.Context, value any) (recovered any) {
	defer func() { recovered = recover() }()
	func() {
		defer runtime.recoverPanic(ctx)
		panic(value)
	}()
	return nil
}

type recordingSentryTransport struct {
	mu         sync.Mutex
	configured bool
	events     []*sentry.Event
	lifecycle  []string
	deadline   time.Duration
}

func (transport *recordingSentryTransport) Configure(sentry.ClientOptions) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.configured = true
}

func (transport *recordingSentryTransport) SendEvent(event *sentry.Event) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.events = append(transport.events, event)
}

func (transport *recordingSentryTransport) Flush(time.Duration) bool { return true }

func (transport *recordingSentryTransport) FlushWithContext(ctx context.Context) bool {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.lifecycle = append(transport.lifecycle, "flush")
	if deadline, ok := ctx.Deadline(); ok {
		transport.deadline = time.Until(deadline)
	}
	return true
}

func (transport *recordingSentryTransport) Close() {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.lifecycle = append(transport.lifecycle, "close")
}

func (transport *recordingSentryTransport) wasConfigured() bool {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.configured
}

func (transport *recordingSentryTransport) snapshot() ([]*sentry.Event, []string, time.Duration) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]*sentry.Event(nil), transport.events...), append([]string(nil), transport.lifecycle...), transport.deadline
}
