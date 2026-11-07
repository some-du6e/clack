package imgcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// MaxFetchBytes caps one public download (encoded bytes).
const MaxFetchBytes = 32 << 20

// ErrBlockedAddr is returned when a URL resolves to a loopback, private,
// link-local (cloud metadata) or otherwise non-public address.
var ErrBlockedAddr = errors.New("imgcache: refusing to fetch from a non-public address")

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

// dialControl runs after DNS resolution, on the address actually dialled,
// so a hostname that resolves (or re-resolves) to 127.0.0.1 is refused.
func dialControl(network, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !PublicIP(ap.Addr()) {
		return ErrBlockedAddr
	}
	return nil
}

var publicClient = &http.Client{
	Timeout: 45 * time.Second,
	Transport: &http.Transport{
		// No proxy: a proxy would dial the target itself and bypass the
		// address check (and a local proxy is itself a loopback address).
		Proxy: nil,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second,
			Control: dialControl}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("imgcache: too many redirects")
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("imgcache: refusing redirect to %s:", req.URL.Scheme)
		}
		return nil
	},
}

// CheckPublicURL reports whether raw may be fetched by PublicFetch: https
// only (no http, file:, data:, javascript: or other schemes), no
// credentials in the URL, and not an IP literal outside public space.
func CheckPublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("imgcache: bad URL")
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("imgcache: refusing %s: URL", u.Scheme)
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !PublicIP(ip) {
		return ErrBlockedAddr
	}
	if h := strings.ToLower(strings.TrimSuffix(u.Hostname(), ".")); h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return ErrBlockedAddr
	}
	return nil
}

// PublicFetch downloads a public image (avatars, emoji, unfurl and
// attachment images, GIFs) without any credentials, refusing non-https
// URLs and non-public addresses (checked on the dialled IP, so DNS
// rebinding doesn't help), and at most MaxFetchBytes.
func PublicFetch(ctx context.Context, raw string) ([]byte, error) {
	return PublicFetchLimit(ctx, raw, MaxFetchBytes)
}

// PublicFetchLimit is PublicFetch with a custom size cap.
func PublicFetchLimit(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if err := CheckPublicURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	resp, err := publicClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("imgcache: http %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, errors.New("imgcache: image too large")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("imgcache: image too large")
	}
	return b, nil
}
