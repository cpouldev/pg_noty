package delivery

import (
	"net"
	"net/http"
	"time"
)

const (
	defaultDialTimeout           = 5 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultTLSHandshakeTimeout   = 5 * time.Second
	defaultResponseHeaderTimeout = 10 * time.Second
	defaultExpectContinue        = time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultRequestTimeout        = 30 * time.Second
	defaultMaxIdleConns          = 100
)

// TransportConfig contains the per-listener network budgets.  Timeout is the
// outer request budget: it covers dialing, TLS, response headers and body read.
// The other fields map directly to http.Transport's phase-specific controls.
type TransportConfig struct {
	Timeout               time.Duration
	DialTimeout           time.Duration
	KeepAlive             time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	ExpectContinueTimeout time.Duration
	IdleConnTimeout       time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	// AllowedDestinations are the operator's SSRF exemptions, already parsed. An exempt network
	// wins over the blocked ranges, so this is the one field here that widens rather than bounds.
	AllowedDestinations []*net.IPNet
}

func (c TransportConfig) normalized() TransportConfig {
	if c.Timeout <= 0 {
		c.Timeout = defaultRequestTimeout
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = defaultDialTimeout
	}
	if c.KeepAlive <= 0 {
		c.KeepAlive = defaultKeepAlive
	}
	if c.TLSHandshakeTimeout <= 0 {
		c.TLSHandshakeTimeout = defaultTLSHandshakeTimeout
	}
	if c.ResponseHeaderTimeout <= 0 {
		c.ResponseHeaderTimeout = defaultResponseHeaderTimeout
	}
	if c.ExpectContinueTimeout <= 0 {
		c.ExpectContinueTimeout = defaultExpectContinue
	}
	if c.IdleConnTimeout <= 0 {
		c.IdleConnTimeout = defaultIdleConnTimeout
	}
	if c.MaxIdleConnsPerHost <= 0 {
		c.MaxIdleConnsPerHost = 1
	}
	if c.MaxIdleConns < c.MaxIdleConnsPerHost {
		c.MaxIdleConns = defaultMaxIdleConns
		if c.MaxIdleConns < c.MaxIdleConnsPerHost {
			c.MaxIdleConns = c.MaxIdleConnsPerHost
		}
	}
	return c
}

// NewTransport builds one transport for one listener.  The transport never
// consults the process-wide default proxy/client, and every connection checks
// its resolved address in ssrfSafeControl immediately before connecting.
func configuredDialer(config TransportConfig) *net.Dialer {
	return &net.Dialer{
		Timeout:   config.DialTimeout,
		KeepAlive: config.KeepAlive,
		Control:   ssrfControlAllowing(config.AllowedDestinations),
	}
}

func NewTransport(config TransportConfig) *http.Transport {
	config = config.normalized()
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           configuredDialer(config).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
		ExpectContinueTimeout: config.ExpectContinueTimeout,
	}
}

func refuseRedirects(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func buildClient(config TransportConfig) (*http.Client, *http.Transport) {
	config = config.normalized()
	transport := NewTransport(config)
	client := &http.Client{
		Transport:     transport,
		Timeout:       config.Timeout,
		CheckRedirect: refuseRedirects,
	}
	return client, transport
}

// NewClient constructs the HTTPFactory client for one listener. Redirects are
// returned as responses so the worker classifies the 3xx directly; a second hop
// would bypass the destination's connect-time SSRF check. The resulting client
// is safe to reuse concurrently for that listener, but is not shared globally.
func NewClient(config TransportConfig) *http.Client {
	client, _ := buildClient(config)
	return client
}
