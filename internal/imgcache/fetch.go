package imgcache

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// MaxFetchBytes caps one public download (encoded bytes).
const MaxFetchBytes = 32 << 20

// ErrBlockedAddr is returned for URLs naming a loopback, private,
// link-local (cloud metadata) or otherwise non-public address.
var ErrBlockedAddr = slack.ErrBlockedAddr

// PublicIP reports whether ip is a globally routable unicast address (see
// slack.PublicIP, which every credential-free fetch dials through).
func PublicIP(ip netip.Addr) bool { return slack.PublicIP(ip) }

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

// PublicFetch downloads an image from message content (avatars, emoji,
// unfurl and attachment images, GIFs) without any credentials. URLs that
// aren't https, carry userinfo or name a loopback/private address are
// refused up front; the rest load like Slack's web client loads them:
// directly from Slack's own CDNs, through Slack's image proxy
// (slack-imgs.com) from anywhere else, so the sender's host never sees the
// request. The dialer only ever connects to public addresses (DNS
// rebinding doesn't help), and at most MaxFetchBytes are read.
func PublicFetch(ctx context.Context, raw string) ([]byte, error) {
	return PublicFetchLimit(ctx, raw, MaxFetchBytes)
}

// PublicFetchLimit is PublicFetch with a custom size cap.
func PublicFetchLimit(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if err := CheckPublicURL(raw); err != nil {
		return nil, err
	}
	b, _, err := slack.FetchPublic(ctx, raw, limit)
	return b, err
}
