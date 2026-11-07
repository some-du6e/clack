package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCookieURL(t *testing.T) {
	ok := []string{
		"https://slack.com/api/auth.test",
		"https://files.slack.com/files-pri/T1-F1/x.png",
		"https://FILES.Slack.COM/files-pri/T1-F1/x.png",
		"https://hackclub.enterprise.slack.com/api/",
		"https://files.slack.com:443/x",
		"wss://wss-primary.slack.com/?token=x",
	}
	bad := []string{
		"http://files.slack.com/x",            // downgrade
		"https://files.slack.com:8443/x",      // other port
		"https://slack.com.evil.io/x",         // suffix trick
		"https://evilslack.com/x",             // no dot boundary
		"https://evil.com/slack.com",          // path, not host
		"https://evil.com/?h=files.slack.com", // query
		"https://evil.com#.slack.com",         // fragment
		"https://files.slack.com@evil.com/x",  // userinfo: host is evil.com
		"https://u:p@files.slack.com/x",       // userinfo on slack itself
		"https://files.slack.com./x",          // trailing dot
		"https://files..slack.com/x",
		"https://xn--slck-loa.com/x",          // punycode look-alike
		"https://slаck.com/x",                 // Cyrillic а
		"https://files.slack.com%2eevil.io/x", // escaped dot
		"https://a.slack-edge.com/x.png",      // Slack CDN, but the d cookie is .slack.com only
		"https://slack-imgs.com/?url=x",
		"https://127.0.0.1/x",
		"https://[::1]/x",
		"//files.slack.com/x", // no scheme
		"files.slack.com/x",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"",
	}
	for _, u := range ok {
		if !CookieURL(u) {
			t.Errorf("CookieURL(%q) = false, want true", u)
		}
	}
	for _, u := range bad {
		if CookieURL(u) {
			t.Errorf("CookieURL(%q) = true, want false", u)
		}
	}
}

func TestSlackOwnedAndHostUnder(t *testing.T) {
	for _, h := range []string{"slack.com", "a.slack-edge.com", "emoji.slack-edge.com", "slack-imgs.com", "files.slack-files.com"} {
		if !SlackOwnedHost(h) {
			t.Errorf("SlackOwnedHost(%q) = false", h)
		}
	}
	for _, h := range []string{"slack-edge.com.evil.io", "evilslack-edge.com", "slack-imgs.co", "", "slack.com."} {
		if SlackOwnedHost(h) {
			t.Errorf("SlackOwnedHost(%q) = true", h)
		}
	}
	if !HostUnder("media2.giphy.com", "giphy.com") || HostUnder("giphy.com.evil.io", "giphy.com") || HostUnder("evilgiphy.com", "giphy.com") {
		t.Error("HostUnder matched the wrong hosts")
	}
}

func TestProxyImage(t *testing.T) {
	same := []string{
		"https://a.slack-edge.com/production-standard-emoji-assets/x.png",
		"https://avatars.slack-edge.com/2024/x_72.jpg",
		"https://secure.gravatar.com/avatar/abc.jpg?s=72",
		"https://slack-imgs.com/?c=1&o1=ro&url=x",
	}
	for _, u := range same {
		if got := ProxyImage(u); got != u {
			t.Errorf("ProxyImage(%q) = %q, want it unchanged", u, got)
		}
	}
	proxied := []string{
		"https://evil.example/track.png?u=me",
		"http://a.slack-edge.com/x.png", // not https
		"https://a.slack-edge.com.evil.io/x.png",
		"https://a.slack-edge.com:8080/x.png",
		"https://user@a.slack-edge.com/x.png",
		"https://files.slack.com/files-pri/x", // authenticated: core.Fetch, never here unproxied
	}
	for _, u := range proxied {
		got := ProxyImage(u)
		if !strings.HasPrefix(got, ImageProxyBase) {
			t.Errorf("ProxyImage(%q) = %q, want the proxy", u, got)
			continue
		}
		pu, err := url.Parse(got)
		if err != nil || pu.Hostname() != "slack-imgs.com" || pu.Query().Get("url") != u {
			t.Errorf("ProxyImage(%q) = %q: proxy URL doesn't round-trip", u, got)
		}
	}
	for _, u := range []string{"javascript:alert(1)", "data:image/png;base64,AAAA", "file:///etc/passwd", "ftp://x/y", "", "/relative.png"} {
		if got := ProxyImage(u); got != "" {
			t.Errorf("ProxyImage(%q) = %q, want refused", u, got)
		}
	}
	if !PublicFetchURL("https://a.slack-edge.com/x") || PublicFetchURL("https://evil.example/x") || PublicFetchURL("http://a.slack-edge.com/x") {
		t.Error("PublicFetchURL")
	}
}

// Download and UploadBytes refuse to put the cookie on a non-Slack URL, and
// never contact that host.
func TestCredentialsStayOnSlack(t *testing.T) {
	var evilHits atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits.Add(1)
	}))
	defer evil.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redir" {
			http.Redirect(w, r, evil.URL+"/stolen", http.StatusFound)
			return
		}
		if r.URL.Path == "/redir307" {
			http.Redirect(w, r, evil.URL+"/stolen", http.StatusTemporaryRedirect)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer api.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-secret", TeamURL: api.URL + "/"})
	ctx := context.Background()

	for _, u := range []string{evil.URL + "/x.png", "https://evil.example/x", "https://files.slack.com.evil.io/x"} {
		if c.CredentialsOK(u) {
			t.Errorf("CredentialsOK(%q) = true", u)
		}
		if _, err := c.Download(ctx, u); !errors.Is(err, ErrUntrustedHost) {
			t.Errorf("Download(%q) = %v, want ErrUntrustedHost", u, err)
		}
		if err := c.UploadBytes(ctx, u, "a.txt", []byte("hi"), nil); !errors.Is(err, ErrUntrustedHost) {
			t.Errorf("UploadBytes(%q) = %v, want ErrUntrustedHost", u, err)
		}
	}
	if !c.CredentialsOK(api.URL+"/files/x") || !c.CredentialsOK("https://files.slack.com/x") {
		t.Error("CredentialsOK refused the API origin or files.slack.com")
	}
	// A trusted URL that redirects off Slack is not followed: no request,
	// with or without the cookie, reaches the other host.
	for _, p := range []string{"/redir", "/redir307"} {
		if resp, err := c.Download(ctx, api.URL+p); err == nil {
			resp.Body.Close()
			t.Errorf("Download(%s) followed a redirect off Slack", p)
		}
		if err := c.UploadBytes(ctx, api.URL+p, "a.txt", []byte("hi"), nil); err == nil {
			t.Errorf("UploadBytes(%s) followed a redirect off Slack", p)
		}
	}
	if n := evilHits.Load(); n != 0 {
		t.Fatalf("the non-Slack host got %d requests", n)
	}
}

func TestCheckRedirectStripsCredentials(t *testing.T) {
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-secret"})
	via := []*http.Request{{URL: mustURL(t, "https://files.slack.com/files-pri/x")}}
	try := func(target string) (*http.Request, error) {
		req := &http.Request{URL: mustURL(t, target), Header: http.Header{}}
		req.Header.Set("Cookie", "d=xoxd-secret")
		req.Header.Set("Authorization", "Bearer xoxc-1")
		return req, c.checkRedirect(req, via)
	}
	if req, err := try("https://files-origin.slack.com/x"); err != nil || req.Header.Get("Cookie") == "" {
		t.Errorf("slack.com hop: err %v, cookie %q (want kept)", err, req.Header.Get("Cookie"))
	}
	if req, err := try("https://a.slack-edge.com/x.png"); err != nil || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
		t.Errorf("slack-edge hop: err %v, cookie %q (want followed, cookie dropped)", err, req.Header.Get("Cookie"))
	}
	for _, target := range []string{"https://evil.example/x", "http://files.slack.com/x", "https://slack.com.evil.io/x", "https://files.slack.com:8443/x"} {
		req, err := try(target)
		if err == nil {
			t.Errorf("redirect to %s followed", target)
		}
		if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
			t.Errorf("redirect to %s kept credentials", target)
		}
	}
	long := make([]*http.Request, 10)
	if err := c.checkRedirect(&http.Request{URL: mustURL(t, "https://slack.com/"), Header: http.Header{}}, long); err == nil {
		t.Error("redirect loop not stopped")
	}
}

// The public client sends no credentials and won't be bounced off Slack's
// CDNs to another host.
func TestPublicFetch(t *testing.T) {
	var evilHits atomic.Int32
	evil := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { evilHits.Add(1) }))
	defer evil.Close()
	var sawCreds atomic.Bool
	cdn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			sawCreds.Store(true)
		}
		if r.URL.Path == "/bounce" {
			http.Redirect(w, r, evil.URL+"/x", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("PNG"))
	}))
	defer cdn.Close()
	old := PublicHTTP
	defer func() { PublicHTTP = old }()
	PublicHTTP = cdn.Client()
	PublicHTTP.CheckRedirect = publicRedirect
	ctx := context.Background()
	b, mime, err := GetNoCreds(ctx, cdn.URL+"/a.png", 10)
	if err != nil || string(b) != "PNG" || mime != "image/png" {
		t.Fatalf("GetNoCreds = %q %q %v", b, mime, err)
	}
	if _, _, err := GetNoCreds(ctx, cdn.URL+"/bounce", 10); err == nil {
		t.Error("public fetch followed a redirect to a non-Slack host")
	}
	if _, _, err := GetNoCreds(ctx, cdn.URL+"/a.png", 2); err == nil {
		t.Error("size cap not enforced")
	}
	for _, u := range []string{"http://a.slack-edge.com/x", "https://user:pw@a.slack-edge.com/x", "file:///etc/passwd"} {
		if _, _, err := GetNoCreds(ctx, u, 10); err == nil {
			t.Errorf("GetNoCreds(%q) accepted", u)
		}
	}
	if evilHits.Load() != 0 || sawCreds.Load() {
		t.Fatalf("evil hits %d, credentials seen %v", evilHits.Load(), sawCreds.Load())
	}
}

// The realtime socket's URL carries the token and its dial sends the
// cookie: a reconnect_url or getWebSocketURL answer off Slack is refused.
func TestRTMRefusesForeignSocket(t *testing.T) {
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-secret"})
	r := &RTM{c: c, Events: make(chan Event, 1)}
	for _, u := range []string{"wss://evil.example/?token=x", "ws://wss-primary.slack.com/", "wss://wss-primary.slack.com.evil.io/"} {
		r.reconnectURL = u
		if err := r.once(context.Background()); !errors.Is(err, ErrUntrustedHost) {
			t.Errorf("once(%q) = %v, want ErrUntrustedHost", u, err)
		}
	}
}

func TestSignInOnlySlackHosts(t *testing.T) {
	for _, ws := range []string{"evil.com", "slack.com.evil.io", "https://evil.com/hackclub.slack.com", "hackclub.slack.com@evil.com", "127.0.0.1:8080"} {
		_, err := SignIn(context.Background(), SignInInput{Workspace: ws, Token: "xoxc-1", Cookie: "xoxd-1"})
		if err == nil || !strings.Contains(err.Error(), "slack.com") {
			t.Errorf("SignIn(workspace %q) = %v, want a slack.com error before any request", ws, err)
		}
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
