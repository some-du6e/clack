package slack

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// Credential-free fetches (PublicHTTP) only dial public addresses: a Slack
// CDN or proxy name that resolves (or re-resolves, DNS rebinding) to
// 127.0.0.1, a LAN address or cloud metadata (169.254.169.254) is refused at
// dial time, on the address actually dialled.

// ErrBlockedAddr is returned when a fetch would dial a loopback, private,
// link-local (cloud metadata) or otherwise non-public address.
var ErrBlockedAddr = errors.New("slack: refusing to fetch from a non-public address")

// PublicIP reports whether ip is a globally routable unicast address.
// Loopback, RFC 1918, CGNAT, link-local (169.254/16 incl. cloud metadata),
// ULA (fc00::/7), multicast, unspecified and documentation ranges are not.
func PublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "255.255.255.255/32",
		"64:ff9b::/96", "64:ff9b:1::/48", "2001:db8::/32", "2002::/16", "fec0::/10", "100::/64",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// dialControl runs after DNS resolution, on the address actually dialled.
func dialControl(network, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !PublicIP(ap.Addr()) {
		return ErrBlockedAddr
	}
	return nil
}

// publicTransport dials public addresses only and ignores HTTP(S)_PROXY:
// a proxy would dial the target itself and bypass the address check (and a
// local proxy is itself a loopback address).
var publicTransport = &http.Transport{
	Proxy: nil,
	DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second,
		Control: dialControl}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          32,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 20 * time.Second,
}
