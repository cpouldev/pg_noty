package delivery

import (
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// ListenerClient owns the one client and transport assigned to a listener.
// It is safe for concurrent requests, and callers keep it for the listener's
// whole lifetime so idle connections can be reused.
type ListenerClient struct {
	Client    *http.Client
	Transport *http.Transport
}

// NewListenerClient constructs the HTTPFactory client consumed by the worker.
// It owns one reusable client/transport for a listener. A non-positive
// concurrency uses one idle connection; transport normalisation supplies the
// remaining explicit timeout defaults.
func NewListenerClient(timeout time.Duration, concurrency int, allowed []*net.IPNet) *ListenerClient {
	client, transport := buildClient(TransportConfig{
		Timeout:             timeout,
		MaxIdleConnsPerHost: concurrency,
		AllowedDestinations: allowed,
	})
	return &ListenerClient{Client: client, Transport: transport}
}

// Do sends a request using this listener's reusable client.  A non-nil
// response has an open body; use DrainResponseBody (or DoAndDrain) before the
// next request to preserve keepalive reuse.
func (c *ListenerClient) Do(request *http.Request) (*http.Response, error) {
	if c == nil || c.Client == nil {
		return nil, errors.New("nil listener client")
	}
	return c.Client.Do(request)
}

// DoAndDrain sends a request and consumes its response body.  The returned
// snippet is capped in memory while the remainder is discarded, then the body
// is closed.  A caller still receives the status and headers for classification.
func (c *ListenerClient) DoAndDrain(request *http.Request, maxSnippet int64) (*http.Response, []byte, error) {
	response, err := c.Do(request)
	if err != nil {
		return nil, nil, err
	}
	snippet, readErr := DrainResponseBody(response, maxSnippet)
	return response, snippet, readErr
}

// DrainResponseBody reads at most maxSnippet bytes for diagnostics, drains the
// rest so net/http may reuse the connection, and always closes Body.  A nil or
// body-less response is a caller error rather than a panic.
func DrainResponseBody(response *http.Response, maxSnippet int64) ([]byte, error) {
	if response == nil || response.Body == nil {
		return nil, errors.New("response has no body")
	}
	if maxSnippet < 0 {
		maxSnippet = 0
	}
	snippet, readErr := io.ReadAll(io.LimitReader(response.Body, maxSnippet))
	_, drainErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return snippet, readErr
	}
	if drainErr != nil {
		return snippet, drainErr
	}
	return snippet, closeErr
}

// CloseIdleConnections lets a listener release its transport resources during
// shutdown without affecting clients belonging to other listeners.
func (c *ListenerClient) CloseIdleConnections() {
	if c != nil && c.Client != nil {
		c.Client.CloseIdleConnections()
	}
}
