//go:build integration

package delivery

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type HTTPReply struct {
	Status         int
	Body           string
	Headers        http.Header
	Hold           <-chan struct{}
	Echo           bool
	Redirect       string
	TransportError bool
}

type HTTPRequest struct {
	Method string
	Header http.Header
	Body   []byte
	Order  int
}

type HTTPFixture struct {
	server *httptest.Server
	mu     sync.Mutex
	plan   []HTTPReply
	seen   []HTTPRequest
}

func newHTTPFixture(t *testing.T, plan ...HTTPReply) *HTTPFixture {
	t.Helper()
	fixture := &HTTPFixture{plan: append([]HTTPReply(nil), plan...)}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *HTTPFixture) URL() string { return f.server.URL }

func (f *HTTPFixture) Requests() []HTTPRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	copyOf := make([]HTTPRequest, len(f.seen))
	copy(copyOf, f.seen)
	return copyOf
}

func (f *HTTPFixture) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	reply := HTTPReply{Status: http.StatusOK}
	f.mu.Lock()
	if len(f.plan) > 0 {
		reply, f.plan = f.plan[0], f.plan[1:]
	}
	f.seen = append(f.seen, HTTPRequest{Method: request.Method, Header: request.Header.Clone(), Body: body, Order: len(f.seen)})
	f.mu.Unlock()
	if reply.Hold != nil {
		<-reply.Hold
	}
	if reply.TransportError {
		if hijacker, ok := response.(http.Hijacker); ok {
			connection, _, err := hijacker.Hijack()
			if err == nil {
				_ = connection.Close()
			}
		}
		return
	}
	for name, values := range reply.Headers {
		for _, value := range values {
			response.Header().Add(name, value)
		}
	}
	if reply.Redirect != "" {
		http.Redirect(response, request, reply.Redirect, http.StatusFound)
		return
	}
	status := reply.Status
	if status == 0 {
		status = http.StatusOK
	}
	response.WriteHeader(status)
	if reply.Echo {
		_, _ = response.Write(body)
		return
	}
	_, _ = response.Write([]byte(reply.Body))
}

func TestHTTPFixtureScriptsResponsesAndCapturesOrder(t *testing.T) {
	hold := make(chan struct{})
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusServiceUnavailable, Body: "retry", Hold: hold}, HTTPReply{Status: http.StatusOK, Echo: true})
	request, err := http.NewRequest(http.MethodPost, fixture.URL(), strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Test", "one")
	done := make(chan struct{})
	go func() { _, _ = http.DefaultClient.Do(request); close(done) }()
	close(hold)
	<-done
	response, err := http.Post(fixture.URL(), "text/plain", strings.NewReader("again"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if got := len(fixture.Requests()); got != 2 {
		t.Fatalf("captured requests=%d, want two", got)
	}
}

func TestHTTPFixtureControlsReorderRedirectAndTransport(t *testing.T) {
	hold := make(chan struct{})
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK, Hold: hold}, HTTPReply{Status: http.StatusOK})
	firstDone := make(chan struct{})
	go func() {
		response, err := http.Post(fixture.URL(), "text/plain", strings.NewReader("first"))
		if err == nil {
			_ = response.Body.Close()
		}
		close(firstDone)
	}()
	for attempt := 0; attempt < 40 && len(fixture.Requests()) < 1; attempt++ {
		time.Sleep(5 * time.Millisecond)
	}
	second, err := http.Post(fixture.URL(), "text/plain", strings.NewReader("second"))
	if err != nil {
		t.Fatal(err)
	}
	_ = second.Body.Close()
	select {
	case <-firstDone:
		t.Fatal("held first request completed before the second request")
	default:
	}
	close(hold)
	<-firstDone
	if requests := fixture.Requests(); len(requests) != 2 || requests[0].Order != 0 || requests[1].Order != 1 {
		t.Fatalf("request capture = %#v, want deterministic order 0,1", requests)
	}

	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { targetCalls++ }))
	defer target.Close()
	redirectFixture := newHTTPFixture(t, HTTPReply{Redirect: target.URL})
	redirectClient := &http.Client{Transport: http.DefaultTransport, CheckRedirect: refuseRedirects}
	redirectResponse, err := redirectClient.Post(redirectFixture.URL(), "text/plain", strings.NewReader("redirect"))
	if err != nil {
		t.Fatal(err)
	}
	_ = redirectResponse.Body.Close()
	if redirectResponse.StatusCode != http.StatusFound || targetCalls != 0 {
		t.Fatalf("redirect status=%d target calls=%d, want 302 and zero", redirectResponse.StatusCode, targetCalls)
	}

	failureFixture := newHTTPFixture(t, HTTPReply{TransportError: true})
	if response, err := http.Post(failureFixture.URL(), "text/plain", strings.NewReader("failure")); err == nil || response != nil {
		t.Fatalf("transport failure response=%v err=%v, want an error and no response", response, err)
	}

	echoFixture := newHTTPFixture(t, HTTPReply{Status: http.StatusAccepted, Echo: true})
	echoResponse, err := http.Post(echoFixture.URL(), "text/plain", strings.NewReader("echo"))
	if err != nil {
		t.Fatal(err)
	}
	defer echoResponse.Body.Close()
	body, err := io.ReadAll(echoResponse.Body)
	if err != nil || string(body) != "echo" || echoResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("echo status=%d body=%q err=%v", echoResponse.StatusCode, body, err)
	}
}
