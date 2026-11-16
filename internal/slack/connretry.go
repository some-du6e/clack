package slack

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"syscall"
)

// Dead keep-alive connections. The API pool keeps up to 16 idle
// connections per host for 5 minutes (tlsfp.NewTransport). When the
// network path under them resets (a VPN tunnel re-established, a Wi-Fi
// switch, a NAT that dropped its state while the Mac slept), they get no
// RST until something is written to them, so the next request on each one
// fails. Go's transport resends only idempotent requests, and every Slack
// call is a POST. Chromium resends any request whose reused socket failed
// before the response started (HttpNetworkTransaction::ShouldResendRequest).
// doPost does the same once, for reads only (readOnlyMethod): a write may
// have reached Slack before the connection died, and must never go twice.

// resetErrs are the errors of a connection that died under a request.
var resetErrs = []error{syscall.ECONNRESET, syscall.EPIPE, syscall.EADDRNOTAVAIL, io.EOF, io.ErrUnexpectedEOF}

// connReset reports whether err is a connection that died under the
// request (reset, closed, or its local address gone).
func connReset(err error) bool {
	if err == nil {
		return false
	}
	for _, e := range resetErrs {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// readOnlyExact are reads whose names the suffix rule misses.
var readOnlyExact = map[string]bool{
	"conversations.history": true, "conversations.replies": true,
	"users.conversations": true, "users.getPresence": true, "users.counts": true,
	"users.profile.getSections": true, "client.counts": true, "client.getWebSocketURL": true,
}

// notReadOnly match the suffix rule but may do something on Slack's side
// (an email token may be minted and sent).
var notReadOnly = map[string]bool{"users.emailtoken.get": true}

// readOnlyMethod reports whether a call only reads, so resending it can't
// do anything twice: the edge cache API (all lookups), and Web API
// methods named *.info, *.list or *.get, plus readOnlyExact.
func readOnlyMethod(method string) bool {
	if strings.HasPrefix(method, "edge:") || readOnlyExact[method] {
		return true
	}
	if notReadOnly[method] {
		return false
	}
	for _, suf := range []string{".info", ".list", ".get"} {
		if strings.HasSuffix(method, suf) {
			return true
		}
	}
	return false
}

// ResetConns closes the idle API connections (and the public client's),
// so the next request dials on the current network path, and forgets the
// guard's backoff of requests that failed on the old one (a Slack error
// answer keeps its backoff). Core calls it on wake and when the network
// addresses change, as Chromium flushes its socket pools on
// ERR_NETWORK_CHANGED; Run calls it when the socket was reset.
func (c *Client) ResetConns() {
	c.HTTP.CloseIdleConnections()
	PublicHTTP.CloseIdleConnections()
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	for k, f := range c.g.fails {
		var se *Error
		if !errors.As(f.err, &se) && !errors.Is(f.err, RateLimited) {
			delete(c.g.fails, k)
		}
	}
}

// CloseIdleConnections forwards to the wrapped transport, so
// http.Client.CloseIdleConnections reaches the pool.
func (t honestTransport) CloseIdleConnections() { closeIdle(t.base) }

// CloseIdleConnections forwards to the wrapped transport.
func (t *desktopTransport) CloseIdleConnections() { closeIdle(t.base) }

func closeIdle(rt http.RoundTripper) {
	if c, ok := rt.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}
