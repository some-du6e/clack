package tlsfp

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"time"
)

// Connection limits for HTTP/1.1. A connection carries one request at a
// time, so where HTTP/2 multiplexed Clack's boot fan-out (up to
// slack.MaxConcurrent = 12 Web API calls in flight, mostly to the team
// host) over one connection, HTTP/1.1 needs a connection per concurrent
// request. 16 per host covers the 12 API calls plus a download or upload
// without queueing; beyond that Go's transport queues requests rather than
// opening more. Keeping as many idle means a burst's connections are
// reused by the next burst instead of being closed and handshaken again,
// and IdleConnTimeout keeps them for 5 minutes between bursts. A browser
// keeps 6 per host; Python urllib, which survived, opened one per request.
const (
	MaxConnsPerHost     = 16
	MaxIdleConnsPerHost = 16
	MaxIdleConns        = 64
)

// NewTransport returns an HTTP/1.1 transport whose https connections are
// d's: the embedded ClientHello, verified, through d's proxy if any. Plain
// http:// requests (only local fakes in tests) use d.Dial and d.Proxy as
// Go's transport would.
//
// It is the only place an http.Transport is built (scan_test.go). Callers
// may tune timeouts on the result but must not touch its dial, proxy or
// TLS fields.
func NewTransport(d *Dialer) *http.Transport {
	return &http.Transport{
		Proxy: func(r *http.Request) (*url.URL, error) {
			// https goes through DialTLSContext, which does its own
			// CONNECT: Go's transport would run crypto/tls inside the
			// tunnel instead of our dialer.
			if d.Proxy == nil || r.URL.Scheme == "https" || IsLoopbackOnly() {
				return nil, nil
			}
			return d.Proxy(r)
		},
		DialContext:    d.tcp,
		DialTLSContext: d.DialTLSContext,
		// never HTTP/2: a non-nil empty map turns it off, and the hello
		// offers only http/1.1 anyway
		ForceAttemptHTTP2:     false,
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
		MaxConnsPerHost:       MaxConnsPerHost,
		MaxIdleConnsPerHost:   MaxIdleConnsPerHost,
		MaxIdleConns:          MaxIdleConns,
		IdleConnTimeout:       5 * time.Minute,
		TLSHandshakeTimeout:   10 * time.Second, // unused with DialTLSContext; Dialer.HandshakeTimeout applies
		ExpectContinueTimeout: time.Second,
	}
}

// pluginDialer is Mixed's dialer toward Slack: system roots, HTTPS_PROXY.
var pluginDialer = &Dialer{Proxy: http.ProxyFromEnvironment}

// Mixed sends requests to Slack's hosts (SlackHost) through NewTransport
// and every other host through Go's default transport, unchanged. It is
// for clients whose URLs come from elsewhere: plugins' fetch, plugin and
// theme downloads, the update check. It decides per request, so every
// redirect hop is routed on its own host.
var Mixed http.RoundTripper = mixed{slack: NewTransport(pluginDialer), other: http.DefaultTransport}

// Client is an http.Client over Mixed, for the places that used
// http.DefaultClient.
var Client = &http.Client{Transport: Mixed}

type mixed struct {
	slack *http.Transport
	other http.RoundTripper
}

func (m mixed) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := CheckURL(r.URL); err != nil { // loopback.go
		return nil, err
	}
	if r.URL != nil && SlackHost(r.URL.Hostname()) {
		return m.slack.RoundTrip(r)
	}
	return m.other.RoundTrip(r)
}

func (m mixed) CloseIdleConnections() {
	m.slack.CloseIdleConnections()
	if c, ok := m.other.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}
