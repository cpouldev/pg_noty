package delivery

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSSRFControlRejectsBlockedAndMappedClasses(t *testing.T) {
	ips := []string{
		"127.0.0.1", "10.20.0.1", "172.20.0.1", "192.168.1.1", "169.254.1.1", "169.254.169.254", "224.0.0.1",
		"::1", "fd00::1", "fe80::1", "ff02::1", "::ffff:127.0.0.1", "::ffff:10.20.0.1", "::ffff:172.20.0.1",
		"::ffff:192.168.1.1", "::ffff:169.254.1.1", "::ffff:169.254.169.254", "::ffff:224.0.0.1",
		// IETF protocol assignments, benchmarking space, and the reserved class including the
		// broadcast address -- all routable-looking to a hostname check and none of them a
		// destination an operator's webhook should reach.
		"192.0.0.8", "198.18.0.1", "240.0.0.1", "255.255.255.255",
		// NAT64 and 6to4 embed a v4 address in a v6 one without the ::ffff: prefix To4 reduces, so
		// each of these reaches 127.0.0.1 while presenting as a public v6 address.
		"64:ff9b::7f00:1", "2002:7f00:0001::",
	}
	for _, ip := range ips {
		t.Run(ip, func(t *testing.T) {
			if err := ssrfSafeControl("tcp", net.JoinHostPort(ip, "443"), nil); err == nil {
				t.Fatalf("resolved blocked address %s was accepted", ip)
			}
		})
	}
}

func TestSSRFControlAcceptsPublicControlAddresses(t *testing.T) {
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888", "::ffff:8.8.8.8"} {
		if err := ssrfSafeControl("tcp", net.JoinHostPort(ip, "443"), nil); err != nil {
			t.Errorf("public address %s rejected: %v", ip, err)
		}
	}
}

func TestSSRFControlDoesNotTrustHostnameText(t *testing.T) {
	for _, address := range []string{"internal.example:443", "localhost:443", "[::ffff:localhost]:443"} {
		err := ssrfSafeControl("tcp", address, nil)
		if err == nil || !strings.Contains(err.Error(), "not an IP") {
			t.Errorf("hostname %q error = %v, want fail-closed IP parse error", address, err)
		}
	}
}

func TestSSRFControlRejectsMalformedResolvedAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "[::1", "example.com:notaport"} {
		if err := ssrfSafeControl("tcp", address, nil); err == nil {
			t.Errorf("malformed address %q was accepted", address)
		}
	}
}

func TestRedirectIsReturnedAndTargetIsNeverContacted(t *testing.T) {
	var targetHits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetHits.Add(1) }))
	defer closeServer(t, target)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer closeServer(t, origin)
	client := localListenerClient(time.Second)
	defer client.CloseIdleConnections()
	response, _, err := client.DoAndDrain(mustRequest(t, origin.URL), 32)
	if err != nil || response.StatusCode != http.StatusFound || targetHits.Load() != 0 {
		t.Fatalf("redirect result status=%v hits=%d err=%v, want 302 and zero target requests", response.StatusCode, targetHits.Load(), err)
	}
}

func TestDrainResponseBodyPreservesKeepAliveAndCapsSnippet(t *testing.T) {
	var newConnections atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0123456789abcdefghijklmnopqrstuvwxyz"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnections.Add(1)
		}
	}
	server.Start()
	defer closeServer(t, server)
	client := localListenerClient(time.Second)
	defer client.CloseIdleConnections()
	for i := 0; i < 2; i++ {
		response, snippet, err := client.DoAndDrain(mustRequest(t, server.URL), 10)
		if err != nil || response.StatusCode != http.StatusOK || string(snippet) != "0123456789" {
			t.Fatalf("request %d status=%v snippet=%q err=%v", i, response.StatusCode, snippet, err)
		}
	}
	if got := newConnections.Load(); got != 1 {
		t.Fatalf("new TCP connections = %d, want one after draining both bodies", got)
	}
}

func closeServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	server.Close() // httptest.Server.Close has no error result to propagate.
}

func localListenerClient(timeout time.Duration) *ListenerClient {
	config := TransportConfig{Timeout: timeout, MaxIdleConnsPerHost: 8}
	transport := newTestTransport(config, (&net.Dialer{}).DialContext)
	return &ListenerClient{
		Transport: transport,
		Client: &http.Client{
			Transport:     transport,
			Timeout:       config.normalized().Timeout,
			CheckRedirect: refuseRedirects,
		},
	}
}

func holdingPipeDialer(started chan<- struct{}) func(context.Context, string, string) (net.Conn, error) {
	return func(_ context.Context, _, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		close(started)
		go func() {
			defer server.Close()
			buffer := make([]byte, 4096)
			for {
				if _, err := server.Read(buffer); err != nil {
					return
				}
			}
		}()
		return client, nil
	}
}

func partialResponseDialer(started chan<- struct{}) func(context.Context, string, string) (net.Conn, error) {
	return func(_ context.Context, _, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		close(started)
		go func() {
			defer server.Close()
			reader := bufio.NewReader(server)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if line == "\r\n" {
					_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 64\r\nConnection: keep-alive\r\n\r\npartial")
					_, _ = server.Read(make([]byte, 1))
					return
				}
			}
		}()
		return client, nil
	}
}

type failingRemainderBody struct {
	readErr               error
	closeErr              error
	closed, remainderRead bool
	reads                 int
}

func (b *failingRemainderBody) Read(p []byte) (int, error) {
	b.reads++
	switch b.reads {
	case 1:
		copy(p, "head")
		return 4, b.readErr
	case 2:
		b.remainderRead = true
		copy(p, "tail")
		return 4, nil
	default:
		return 0, io.EOF
	}
}
func (b *failingRemainderBody) Close() error { b.closed = true; return b.closeErr }
