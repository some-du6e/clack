package tlsfp

import (
	"errors"
	"net"
	"net/url"
	"sync/atomic"
)

// Loopback-only mode (`clack demo`): a one-way, process-wide switch after
// which every connection this package's dialers and transports open, and
// every request Mixed or package slack's transports send, must go to a
// loopback host (localhost, 127.0.0.0/8, ::1). Proxies are ignored, so the
// dialed address is always the request's own host. The demo runs against
// internal/fakeslack on 127.0.0.1 and must never reach Slack.

var loopbackOnly atomic.Bool

// ErrNotLoopback is what a request or dial to any other host gets in
// loopback-only mode.
var ErrNotLoopback = errors.New("demo mode: refusing to connect to a non-loopback host")

// LoopbackOnly turns loopback-only mode on for the rest of the process.
// There is no way back.
func LoopbackOnly() { loopbackOnly.Store(true) }

// IsLoopbackOnly reports whether LoopbackOnly was called.
func IsLoopbackOnly() bool { return loopbackOnly.Load() }

// LoopbackHost reports whether host (a name or an IP, no port) is
// localhost or a loopback address. Other names are not resolved.
func LoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckURL is nil unless loopback-only mode is on and u isn't on a
// loopback host.
func CheckURL(u *url.URL) error {
	if !loopbackOnly.Load() {
		return nil
	}
	if u == nil || !LoopbackHost(u.Hostname()) {
		return ErrNotLoopback
	}
	return nil
}

// CheckAddr is CheckURL for a dial address (host:port).
func CheckAddr(addr string) error {
	if !loopbackOnly.Load() {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || !LoopbackHost(host) {
		return ErrNotLoopback
	}
	return nil
}
