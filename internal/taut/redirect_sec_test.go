package taut

import (
	"net/http"
	"net/url"
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
	if fetchRedirect(&http.Request{URL: parse("https://example.com/"), Header: http.Header{}}, make([]*http.Request, 10)) == nil {
		t.Error("redirect loop not stopped")
	}
}
