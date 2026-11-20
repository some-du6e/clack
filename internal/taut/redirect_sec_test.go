package taut

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The default fetch client never follows a redirect that would carry a
// hand-set Cookie or Authorization header to a Slack page. Go copies them
// to a hop on the same host, so checkAuth's auth.test answered with a 302
// to /ssb/redirect would otherwise load the sign-out page with the saved
// account's cookie.
func TestFetchRedirectKeepsCredentialsOffSlackPages(t *testing.T) {
	parse := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatalf("parse %s: %v", s, err)
		}
		return u
	}
	via := []*http.Request{{Method: http.MethodPost, URL: parse("https://hackclub.slack.com/api/auth.test")}}
	hop := func(target, cookie, auth string) (*http.Request, error) {
		req := &http.Request{Method: http.MethodGet, URL: parse(target), Header: http.Header{}}
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		return req, fetchRedirect(req, via)
	}
	for _, target := range []string{
		"https://hackclub.slack.com/ssb/redirect",
		"https://hackclub.slack.com/signin",
		"https://files.slack.com/ssb/redirect",
		"https://app.slack.com/client",
		"https://hackclub.slack.com/files-pri/%2e%2e/ssb/redirect",
		// Go copies the cookie to these too (it compares host names only):
		// plain http, userinfo and another port are never a cookie hop.
		"http://hackclub.slack.com/ssb/redirect",
		"https://x@hackclub.slack.com/ssb/redirect",
		"https://hackclub.slack.com:8443/files-pri/T1-F1/x.png",
		"http://files.slack.com/files-pri/T1-F1/x.png",
	} {
		for _, creds := range [][2]string{{"d=xoxd-1", ""}, {"", "Bearer xoxc-1"}} {
			req, err := hop(target, creds[0], creds[1])
			if err == nil {
				t.Errorf("redirect to %s with credentials %q followed", target, creds)
			}
			if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
				t.Errorf("redirect to %s kept credentials", target)
			}
		}
	}
	// A file hop keeps the cookie, and a hop without credentials (a
	// plugin's plain fetch) is followed as before.
	if req, err := hop("https://files.slack.com/files-pri/T1-F1/x.png", "d=xoxd-1", ""); err != nil || req.Header.Get("Cookie") == "" {
		t.Errorf("file hop: err %v, cookie %q (want followed, kept)", err, req.Header.Get("Cookie"))
	}
	for _, target := range []string{"https://hackclub.slack.com/ssb/redirect", "https://example.com/x"} {
		if _, err := hop(target, "", ""); err != nil {
			t.Errorf("credential-free hop to %s: %v (want followed)", target, err)
		}
	}
	// Off Slack, a hop keeps what Go copied: a plugin's own token on a
	// same-host 301 at api.github.com is the plugin's business.
	gh := []*http.Request{{Method: http.MethodGet, URL: parse("https://api.github.com/repos/o/old")}}
	req := &http.Request{Method: http.MethodGet, URL: parse("https://api.github.com/repositories/1"), Header: http.Header{}}
	req.Header.Set("Authorization", "Bearer ghp-1")
	if err := fetchRedirect(req, gh); err != nil || req.Header.Get("Authorization") != "Bearer ghp-1" {
		t.Errorf("api.github.com hop: err %v, Authorization %q (want followed, kept)", err, req.Header.Get("Authorization"))
	}
	// A 307 or 308 keeps a credentialed POST where slack.Client's own
	// redirects keep it (slack.EndpointHop): another Web API method or the
	// first request's path, never a page, a sign-out method or plain http.
	post := func(target, first string) (*http.Request, error) {
		req := &http.Request{Method: http.MethodPost, URL: parse(target), Header: http.Header{}}
		req.Header.Set("Cookie", "d=xoxd-1")
		return req, fetchRedirect(req, []*http.Request{{Method: http.MethodPost, URL: parse(first)}})
	}
	for _, c := range [][2]string{
		{"https://hackclub.slack.com/api/auth.test", "https://slack.com/api/auth.test"},
		{"https://slack.com/api/chat.postMessage", "https://hackclub.slack.com/api/chat.postMessage"},
		{"https://files.slack.com/upload/v1/ABC?retry=1", "https://files.slack.com/upload/v1/ABC"},
	} {
		if req, err := post(c[0], c[1]); err != nil || req.Header.Get("Cookie") == "" {
			t.Errorf("POST hop %s -> %s: err %v, cookie %q (want followed, kept)", c[1], c[0], err, req.Header.Get("Cookie"))
		}
	}
	for _, target := range []string{
		"https://hackclub.slack.com/ssb/redirect",
		"https://hackclub.slack.com/api/../ssb/redirect",
		"https://hackclub.slack.com/api/auth.signout",
		"https://files.slack.com/upload/v1/OTHER",
		"http://hackclub.slack.com/api/auth.test",
	} {
		if req, err := post(target, "https://slack.com/api/auth.test"); err == nil || req.Header.Get("Cookie") != "" {
			t.Errorf("POST hop to %s: err %v, cookie %q (want refused, removed)", target, err, req.Header.Get("Cookie"))
		}
	}
	if fetchRedirect(&http.Request{URL: parse("https://example.com/"), Header: http.Header{}}, make([]*http.Request, 10)) == nil {
		t.Error("redirect loop not stopped")
	}
}

// A plugin's first request may carry a cookie or token it set anywhere on
// Slack (plugins are trusted code, docs/SECURITY.md P-13) except the
// sign-out redirect page and the Web API methods that end the session,
// however the path is written: those are refused before anything is sent.
func TestPluginFetchRefusesSignOut(t *testing.T) {
	var sent []string
	fetch := honestFetch(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		sent = append(sent, req.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
	})
	try := func(method, u, cookie, auth string) error {
		req, err := http.NewRequest(method, u, nil)
		if err != nil {
			t.Fatalf("NewRequest(%s): %v", u, err)
		}
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		res, err := fetch(context.Background(), req)
		if err == nil {
			res.Body.Close()
		}
		return err
	}
	for _, u := range []string{
		"https://hackclub.slack.com/ssb/redirect",
		"https://hackclub.slack.com/SSB/Redirect/",
		"https://hackclub.slack.com/ssb/%252fredirect",
		"https://files.slack.com/files-pri/../ssb/redirect",
		"http://hackclub.slack.com/ssb/redirect",
		"https://hackclub.slack.com/api/auth.signout",
		"https://slack.com/api/AUTH.REVOKE",
		"https://hackclub.enterprise.slack.com/api/auth.enterpriseSignout",
		"https://hackclub.slack.com/api/%61uth.signout",
		"https://hackclub.slack.com/api//auth.signout/",
		// a query or ";" after the method name, and a fullwidth "a" that
		// NFKC-folds to one: slack.SignOutURL reads both as auth.signout
		"https://hackclub.slack.com/api/auth.signout%3Fx=1",
		"https://hackclub.slack.com/api/%EF%BD%81uth.signout",
	} {
		for _, creds := range [][2]string{{"d=xoxd-1", ""}, {"", "Bearer xoxc-1"}} {
			sent = nil
			if err := try(http.MethodGet, u, creds[0], creds[1]); err == nil || len(sent) != 0 {
				t.Errorf("credentialed fetch of %s %q: err %v, sent %v (want refused, nothing sent)", u, creds, err, sent)
			}
		}
	}
	// Everything else goes out: a plugin's credentialed page load, Web API
	// call or file, any page without credentials, and other hosts.
	for _, c := range []struct{ method, url, cookie, auth string }{
		{http.MethodGet, "https://hackclub.slack.com/customize/emoji", "d=xoxd-1", ""},
		{http.MethodPost, "https://hackclub.slack.com/api/users.info", "d=xoxd-1", ""},
		{http.MethodPost, "https://hackclub.slack.com/api/auth.test", "d=xoxd-1", ""},
		{http.MethodGet, "https://files.slack.com/files-pri/T1-F1/x.png", "d=xoxd-1", ""},
		{http.MethodGet, "https://hackclub.slack.com/ssb/signin_redirect", "d=xoxd-1", ""},
		{http.MethodGet, "https://hackclub.slack.com/ssb/redirect", "", ""},
		{http.MethodGet, "https://api.github.com/repos/o/r", "", "Bearer ghp-1"},
		{http.MethodGet, "https://example.com/ssb/redirect", "d=xoxd-1", ""},
	} {
		sent = nil
		if err := try(c.method, c.url, c.cookie, c.auth); err != nil || len(sent) != 1 {
			t.Errorf("fetch of %s: err %v, sent %v (want sent once)", c.url, err, sent)
		}
	}
}
