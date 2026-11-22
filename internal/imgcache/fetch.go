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

// CheckPublicURL reports whether raw may be fetched by PublicFetch: http
// or https (no file:, data:, javascript: or other schemes), no credentials
// in the URL, and not an IP literal outside public space. An http URL is
// never fetched as it is: like any URL off Slack's own hosts it loads
// through Slack's image proxy, as Slack's client loads it
// (getImgProxyUrl, client-boot d0up: only an https URL on one of Slack's
// hosts loads directly; anything else, plain http included, is the
// proxy's ?c=1&o1=…&url=).
func CheckPublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("imgcache: bad URL")
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
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
// unfurl and attachment images, image blocks, GIFs) without any
// credentials. URLs that aren't http(s), carry userinfo or name a
// loopback/private address are refused up front; the rest load like
// Slack's web client loads them: https ones directly from Slack's own
// CDNs, everything else (any other host, any http URL) through Slack's
// image proxy (slack-imgs.com), so the sender's host never sees the
// request and nothing goes out in the clear. The dialer only ever connects
// to public addresses (DNS rebinding doesn't help), and at most
// MaxFetchBytes are read.
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

// SessionFetch is a signed-in session's image fetcher (the image cache's,
// cmd/clack): a URL the session may send its credentials to (credsOK:
// https slack.com hosts, files.slack.com thumbnails…) loads through
// authed (core.Fetch, kept in memory); anything else loads with no
// credentials through PublicFetch.
func SessionFetch(credsOK func(string) bool, authed Fetcher) Fetcher {
	return func(ctx context.Context, raw string) ([]byte, error) {
		if credsOK(raw) {
			return authed(ctx, raw)
		}
		return PublicFetch(ctx, raw)
	}
}
