package delivery

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewTransportSetsEveryTimeoutAndListenerCap(t *testing.T) {
	want := TransportConfig{Timeout: 7 * time.Second, DialTimeout: 11 * time.Second, KeepAlive: 13 * time.Second, TLSHandshakeTimeout: 17 * time.Second, ResponseHeaderTimeout: 19 * time.Second, ExpectContinueTimeout: 23 * time.Second, IdleConnTimeout: 29 * time.Second, MaxIdleConns: 20, MaxIdleConnsPerHost: 9}
	config, dialer := want.normalized(), configuredDialer(want.normalized())
	for name, got := range map[string]time.Duration{"client": config.Timeout, "dial": config.DialTimeout, "keep-alive": config.KeepAlive, "tls": config.TLSHandshakeTimeout, "headers": config.ResponseHeaderTimeout, "expect": config.ExpectContinueTimeout, "idle": config.IdleConnTimeout} {
		if got <= 0 {
			t.Errorf("%s timeout = %s, want positive", name, got)
		}
	}
	if dialer.Timeout != want.DialTimeout || dialer.KeepAlive != want.KeepAlive {
		t.Fatalf("dialer = timeout %s keep-alive %s, want %s/%s", dialer.Timeout, dialer.KeepAlive, want.DialTimeout, want.KeepAlive)
	}
	client := NewClient(want)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if client.Timeout != want.Timeout || transport.TLSHandshakeTimeout != want.TLSHandshakeTimeout || transport.ResponseHeaderTimeout != want.ResponseHeaderTimeout || transport.ExpectContinueTimeout != want.ExpectContinueTimeout || transport.IdleConnTimeout != want.IdleConnTimeout || transport.MaxIdleConns != want.MaxIdleConns || transport.MaxIdleConnsPerHost != want.MaxIdleConnsPerHost || transport.DialContext == nil || transport.Proxy != nil {
		t.Fatalf("transport does not preserve explicit config: client=%s transport=%+v", client.Timeout, transport)
	}
	if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect callback = %v, want ErrUseLastResponse", err)
	}
	first, second := NewListenerClient(want.Timeout, 4, nil), NewListenerClient(want.Timeout, 7, nil)
	defer first.CloseIdleConnections()
	defer second.CloseIdleConnections()
	if first.Client == nil || first.Transport == nil || first.Client.Transport != first.Transport || second.Client == nil || second.Transport == nil || second.Client.Transport != second.Transport || first.Client == second.Client || first.Transport == second.Transport {
		t.Fatal("listener factory did not isolate clients and transports")
	}
	if first.Transport.MaxIdleConnsPerHost != 4 || second.Transport.MaxIdleConnsPerHost != 7 {
		t.Fatalf("listener caps = %d/%d, want 4/7", first.Transport.MaxIdleConnsPerHost, second.Transport.MaxIdleConnsPerHost)
	}
	if err := first.Client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("listener redirect callback = %v, want ErrUseLastResponse", err)
	}
}
func TestProductionTransportRunsConnectTimeSSRFControl(t *testing.T) {
	client := NewClient(TransportConfig{Timeout: time.Second})
	_, err := client.Get("http://127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "blocked address") {
		t.Fatalf("loopback request error = %v, want connect-time blocked-address refusal", err)
	}
}
func TestProductionConstructorsCannotBypassSSRF(t *testing.T) {
	listener := NewListenerClient(time.Second, 2, nil)
	defer listener.CloseIdleConnections()
	clients := map[string]*http.Client{"client": NewClient(TransportConfig{Timeout: time.Second}), "listener": listener.Client, "transport": {Transport: NewTransport(TransportConfig{Timeout: time.Second}), Timeout: time.Second}}
	for name, client := range clients {
		_, err := client.Get("http://127.0.0.1:1")
		if err == nil || !strings.Contains(err.Error(), "blocked address") {
			t.Errorf("%s constructor error = %v, want connect-time blocked-address refusal", name, err)
		}
		client.CloseIdleConnections()
	}
}
func TestStalledDialHonorsListenerTimeout(t *testing.T) {
	started := make(chan struct{})
	const phaseTimeout = 20 * time.Millisecond
	client := newTestClient(TransportConfig{Timeout: phaseTimeout}, stalledDialer(started))
	_, err := awaitClientError(t, started, phaseTimeout, phaseTimeout*20, func() error { _, err := client.Do(mustRequest(t, "http://dial-stall.test")); return err })
	if err == nil || !strings.Contains(err.Error(), "Client.Timeout exceeded while awaiting headers") {
		t.Fatalf("dial timeout error = %v, want listener timeout while awaiting headers", err)
	}
}
func TestTLSHandshakeTimeoutUsesControlledPipe(t *testing.T) {
	started := make(chan struct{})
	const phaseTimeout = 20 * time.Millisecond
	client := newTestClient(TransportConfig{Timeout: time.Second, TLSHandshakeTimeout: phaseTimeout}, holdingPipeDialer(started))
	_, err := awaitClientError(t, started, phaseTimeout, phaseTimeout*20, func() error { _, err := client.Do(mustRequest(t, "https://tls-stall.test")); return err })
	if err == nil || !strings.Contains(err.Error(), "TLS handshake timeout") {
		t.Fatalf("TLS result = %v, want handshake timeout", err)
	}
}
func TestResponseHeaderTimeoutUsesControlledPipe(t *testing.T) {
	started := make(chan struct{})
	const phaseTimeout = 20 * time.Millisecond
	client := newTestClient(TransportConfig{Timeout: time.Second, ResponseHeaderTimeout: phaseTimeout}, holdingPipeDialer(started))
	_, err := awaitClientError(t, started, phaseTimeout, phaseTimeout*20, func() error { _, err := client.Do(mustRequest(t, "http://header-stall.test")); return err })
	if err == nil || !strings.Contains(err.Error(), "timeout awaiting response headers") {
		t.Fatalf("response-header timeout error = %v, want ResponseHeaderTimeout", err)
	}
}
func TestResponseBodyTimeoutUsesPartialControlledResponse(t *testing.T) {
	started := make(chan struct{})
	const phaseTimeout = 30 * time.Millisecond
	client := newTestClient(TransportConfig{Timeout: phaseTimeout, ResponseHeaderTimeout: time.Second}, partialResponseDialer(started))
	_, err := awaitClientError(t, started, phaseTimeout, phaseTimeout*20, func() error {
		response, err := client.Do(mustRequest(t, "http://body-stall.test"))
		if err == nil {
			_, err = DrainResponseBody(response, 32)
		}
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "reading body") {
		t.Fatalf("response-body timeout error = %v, want client body deadline", err)
	}
}
func awaitClientError(t *testing.T, started <-chan struct{}, phaseTimeout, leaseTimeout time.Duration, run func() error) (time.Duration, error) {
	t.Helper()
	startedAt := time.Now()
	done := make(chan error, 1)
	go func() { done <- run() }()
	<-started
	select {
	case err := <-done:
		elapsed := time.Since(startedAt)
		if elapsed < phaseTimeout/2 {
			t.Fatalf("controlled timeout took %s, want at least half of configured phase timeout %s", elapsed, phaseTimeout)
		}
		// Controlled pipes should complete at the configured deadline; 2x leaves
		// a small scheduler margin while binding the assertion to that deadline.
		phaseUpper := phaseTimeout * 2
		if elapsed >= phaseUpper {
			t.Fatalf("controlled timeout took %s, not before phase upper bound %s (configured %s)", elapsed, phaseUpper, phaseTimeout)
		}
		if elapsed >= leaseTimeout {
			t.Fatalf("controlled timeout took %s, not before lease timeout %s", elapsed, leaseTimeout)
		}
		return elapsed, err
	case <-time.After(leaseTimeout):
		t.Fatal("controlled timeout exceeded lease bound")
		return leaseTimeout, nil
	}
}
func TestDrainResponseBodyReturnsReadErrorAndClosesAfterRemainder(t *testing.T) {
	body := &failingRemainderBody{readErr: errors.New("first read failed")}
	snippet, err := DrainResponseBody(&http.Response{Body: body}, 16)
	if !errors.Is(err, body.readErr) || string(snippet) != "head" {
		t.Fatalf("snippet=%q err=%v, want head and first-read error", snippet, err)
	}
	if !body.closed || !body.remainderRead {
		t.Fatalf("body closed=%t remainderRead=%t, want close and deterministic drain", body.closed, body.remainderRead)
	}
}
func TestDrainResponseBodyReturnsCloseError(t *testing.T) {
	want := errors.New("close failed")
	_, err := DrainResponseBody(&http.Response{Body: &failingRemainderBody{closeErr: want}}, 16)
	if !errors.Is(err, want) {
		t.Fatalf("close error = %v, want %v", err, want)
	}
}

func newTestTransport(config TransportConfig, dial func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	config = config.normalized()
	transport := NewTransport(config)
	transport.DialContext = dial
	return transport
}
func newTestClient(config TransportConfig, dial func(context.Context, string, string) (net.Conn, error)) *http.Client {
	config = config.normalized()
	return &http.Client{Transport: newTestTransport(config, dial), Timeout: config.Timeout, CheckRedirect: refuseRedirects}
}
func stalledDialer(started chan<- struct{}) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
}
func mustRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
