//go:build integration

package delivery

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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
	f.seen = append(f.seen, HTTPRequest{Method: request.Method, Header: request.Header.Clone(), Body: body})
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

func TestRedirectPolicyRefusesFollowingLocation(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { targetCalls++ }))
	defer target.Close()
	fixture := newHTTPFixture(t, HTTPReply{Redirect: target.URL})
	client := &http.Client{Transport: http.DefaultTransport, CheckRedirect: refuseRedirects}
	response, err := client.Post(fixture.URL(), "text/plain", strings.NewReader("redirect"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound || targetCalls != 0 {
		t.Fatalf("redirect status=%d target calls=%d, want 302 and zero", response.StatusCode, targetCalls)
	}
}
