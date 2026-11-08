package slack

import (
	"net/http"
	"strings"
	"sync/atomic"
)

// Clack tells Slack what it is. Every request it makes to Slack (Web API,
// edgeapi, files, uploads, the sign-in exchange and token page, the
// realtime socket, credential-free CDN and image fetches) carries one
// User-Agent, UserAgent(), and none of the headers only a browser sends.
//
// Why (docs/SIGNOUT-AUDIT.md, live-tested 2026-11-08): Hack Club's
// Enterprise Grid ends a session everywhere (browser, desktop, every
// client) when a request that claims to be a browser plainly isn't one.
// Clack's Go TLS and HTTP/2 stack is not Chrome's, so a Chrome User-Agent
// with client hints (pasted sessions) and Slack desktop's Electron
// User-Agent (desktop sessions) both signed the test account out within
// seconds. A plain script with its default, honest User-Agent made the
// same requests (magic-link exchange, token page, auth.test) and both
// sessions stayed alive. So Clack doesn't pretend.

// userAgentProduct and userAgentComment make up UserAgent().
//
// The form is RFC 9110 §10.1.5's product/version plus a comment, with a
// "+URL" contact as crawlers and API clients write it: it names the app
// (so a Slack or workspace admin reading access logs knows what this is
// and where to look) and says nothing about a browser, an engine or an OS.
// It is the same on every OS and for every session kind, so Slack always
// sees one consistent client.
const (
	userAgentProduct = "Clack"
	userAgentComment = "(+https://github.com/some-du6e/yoloslackgoclient)"
)

var userAgent atomic.Value // string

func init() { SetVersion("dev") }

// SetVersion sets the version in UserAgent (cmd/clack's build version, e.g.
// v0.1.0; a leading v is dropped). Anything that isn't a plain version
// token becomes "dev". Call it before signing in or connecting.
func SetVersion(v string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" || len(v) > 64 || strings.IndexFunc(v, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '.' || r == '-' || r == '+' || r == '_')
	}) >= 0 {
		v = "dev"
	}
	userAgent.Store(userAgentProduct + "/" + v + " " + userAgentComment)
}

// UserAgent is the User-Agent of every request Clack makes to Slack, e.g.
// "Clack/0.1.0 (+https://github.com/some-du6e/yoloslackgoclient)".
func UserAgent() string { return userAgent.Load().(string) }

// browserUATokens is the deny list of browser identities: none of these
// may appear in UserAgent (TestUserAgentIsHonest) or in anything Clack
// sends Slack (TestNoBrowserDisguise). The source scan in ua_test.go allows
// these strings in non-test code only inside this declaration.
var browserUATokens = []string{"Mozilla/", "AppleWebKit", "Chrome", "Safari", "Electron", "Slack/", "Slack_SSB"}

// BrowserOnlyHeader reports whether a request header named k is one only a
// browser sends: client hints (Sec-CH-*), fetch metadata (Sec-Fetch-*),
// Origin and Referer. Clack sends none of them to Slack.
func BrowserOnlyHeader(k string) bool {
	k = strings.ToLower(k)
	return strings.HasPrefix(k, "sec-ch-") || strings.HasPrefix(k, "sec-fetch-") || k == "origin" || k == "referer"
}

// HonestHeaders sets h's User-Agent to UserAgent() and removes every
// browser-only header (BrowserOnlyHeader).
func HonestHeaders(h http.Header) {
	for k := range h {
		if BrowserOnlyHeader(k) {
			delete(h, k)
		}
	}
	h.Set("User-Agent", UserAgent())
}

// honestTransport applies HonestHeaders to every request it carries, so
// redirect hops, the websocket handshake and any header a caller added
// leave Clack the same way. It wraps every transport that talks to Slack:
// Client.HTTP's, the sign-in exchange's and PublicHTTP's.
type honestTransport struct{ base http.RoundTripper }

func (t honestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !honestAlready(req.Header) {
		r := req.Clone(req.Context()) // a RoundTripper mustn't change the caller's request
		HonestHeaders(r.Header)
		req = r
	}
	return t.base.RoundTrip(req)
}

func honestAlready(h http.Header) bool {
	if v := h.Values("User-Agent"); len(v) != 1 || v[0] != UserAgent() {
		return false
	}
	for k := range h {
		if BrowserOnlyHeader(k) {
			return false
		}
	}
	return true
}
