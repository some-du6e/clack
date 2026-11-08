package slack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Which hosts get what. Message content (files, unfurls, blocks, bot icons,
// emoji, canvases, plugin CSS) can carry any URL, so every fetch Clack makes
// on its own goes through these rules:
//
//   - The session cookie and token go only to https slack.com and its
//     subdomains (CookieURL), which is how the browser scopes Slack's d
//     cookie (.slack.com). Not slack-edge.com, not look-alikes such as
//     slack.com.evil.io or evil.com/slack.com, not IDN homoglyphs.
//   - Images on Slack's own CDNs load directly; every other image goes
//     through Slack's image proxy (ProxyImage), as the web client does, so a
//     sender can't learn your IP address by posting an image URL.
//   - Redirects that leave CookieURL drop the cookie, and redirects that
//     leave Slack's hosts are refused (see checkRedirect).

// normHost lower-cases host and reports whether it is a plain ASCII DNS
// name: letters, digits, '-' and '.', no empty labels, no trailing dot.
// IP literals with ports, userinfo, IDN (non-ASCII) and anything else fail.
func normHost(host string) (string, bool) {
	if host == "" || len(host) > 253 {
		return "", false
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return "", false
		}
	}
	h := strings.ToLower(host)
	if h[0] == '.' || h[len(h)-1] == '.' || strings.Contains(h, "..") {
		return "", false
	}
	return h, true
}

func hostIs(h, domain string) bool { return h == domain || strings.HasSuffix(h, "."+domain) }

// HostUnder reports whether host is domain or a subdomain of it, after the
// same normalisation as CookieHost (so "evil.com/giphy.com" or
// "giphy.com.evil.io" never match "giphy.com").
func HostUnder(host, domain string) bool {
	h, ok := normHost(host)
	return ok && hostIs(h, domain)
}

// CookieHost reports whether host is slack.com or one of its subdomains,
// the only hosts that may see the session cookie or token.
func CookieHost(host string) bool {
	h, ok := normHost(host)
	return ok && hostIs(h, "slack.com")
}

// SlackOwnedHost reports whether host belongs to Slack: slack.com,
// slack-edge.com (CDN), slack-imgs.com (image proxy), slack-files.com and
// their subdomains. Fetching from these leaks nothing Slack doesn't already
// see, but only CookieHost hosts get credentials.
func SlackOwnedHost(host string) bool {
	h, ok := normHost(host)
	if !ok {
		return false
	}
	for _, d := range []string{"slack.com", "slack-edge.com", "slack-imgs.com", "slack-files.com"} {
		if hostIs(h, d) {
			return true
		}
	}
	return false
}

// CookieURL reports whether the session cookie may be sent with a request
// to raw: https (or wss), no userinfo, default port, and a CookieHost.
func CookieURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && cookieURL(u)
}

func cookieURL(u *url.URL) bool {
	if u == nil || (u.Scheme != "https" && u.Scheme != "wss") || u.User != nil || u.Opaque != "" {
		return false
	}
	if p := u.Port(); p != "" && p != "443" {
		return false
	}
	return CookieHost(u.Hostname())
}

func slackOwnedURL(u *url.URL) bool {
	if u == nil || (u.Scheme != "https" && u.Scheme != "wss") || u.User != nil || u.Opaque != "" {
		return false
	}
	if p := u.Port(); p != "" && p != "443" {
		return false
	}
	return SlackOwnedHost(u.Hostname())
}

// directImageHosts are third-party hosts Slack's own client loads images
// from directly (profile pictures are Gravatar URLs with a slack-edge
// fallback), so loading them reveals nothing new.
// i0–i3.wp.com are Automattic's image proxy, where Gravatar redirects a
// missing picture's d= fallback.
var directImageHosts = map[string]bool{"secure.gravatar.com": true,
	"i0.wp.com": true, "i1.wp.com": true, "i2.wp.com": true, "i3.wp.com": true}

// ImageProxyBase is Slack's image proxy, which the web client puts in
// front of third-party images (unfurls, attachments, bot icons).
const ImageProxyBase = "https://slack-imgs.com/?c=1&o1=ro&url="

// ProxyImage returns the URL to load an image from without credentials:
// raw itself for https URLs on Slack's own hosts (and Gravatar), otherwise
// raw wrapped in Slack's image proxy. It returns "" for anything that
// isn't http(s). CookieURL images still have to go through core.Fetch to
// be authenticated; ProxyImage is for the unauthenticated path.
func ProxyImage(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	if u.Scheme == "https" && u.User == nil && (u.Port() == "" || u.Port() == "443") {
		if SlackOwnedHost(u.Hostname()) {
			return raw
		}
		if h, ok := normHost(u.Hostname()); ok && directImageHosts[h] {
			return raw
		}
	}
	return ImageProxyBase + url.QueryEscape(raw)
}

// PublicFetchURL reports whether an unauthenticated fetch of raw is
// allowed on its own: https on a Slack-owned host or a direct image host.
// Anything else must be wrapped with ProxyImage first.
func PublicFetchURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if slackOwnedURL(u) {
		return true
	}
	h, ok := normHost(u.Hostname())
	return ok && u.Scheme == "https" && u.User == nil && (u.Port() == "" || u.Port() == "443") && directImageHosts[h]
}

// PublicHTTP is the client for fetches that carry no credentials. It never
// sends cookies, and follows redirects only to URLs PublicFetchURL allows,
// so a proxied or CDN URL can't bounce the request to a sender's host.
// Its transport dials public addresses only (netguard.go).
var PublicHTTP = &http.Client{Timeout: 45 * time.Second, Transport: honestTransport{publicTransport}, CheckRedirect: publicRedirect}

func publicRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("slack: stopped after 10 redirects")
	}
	req.Header.Del("Cookie")
	req.Header.Del("Authorization")
	if !PublicFetchURL(req.URL.String()) {
		return fmt.Errorf("slack: refusing redirect to %q", req.URL.Hostname())
	}
	return nil
}

// GetNoCreds GETs raw exactly, over https, with no cookie or token, and
// returns at most max bytes and the content type. Callers decide whether
// raw may be loaded directly; for images from message content, use
// FetchPublic instead.
func GetNoCreds(ctx context.Context, raw string, max int64) ([]byte, string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return nil, "", fmt.Errorf("slack: refusing to fetch %q", raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	HonestHeaders(req.Header)
	resp, err := PublicHTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("GET %s: %s", u.Hostname(), resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > max {
		return nil, "", errors.New("slack: response too large")
	}
	return b, resp.Header.Get("Content-Type"), nil
}

// FetchPublic loads an image without credentials: directly from Slack's
// hosts (and Gravatar), through Slack's image proxy from anywhere else.
func FetchPublic(ctx context.Context, raw string, max int64) ([]byte, string, error) {
	u := ProxyImage(raw)
	if u == "" {
		return nil, "", fmt.Errorf("slack: not an http(s) URL: %q", raw)
	}
	return GetNoCreds(ctx, u, max)
}
