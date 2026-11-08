package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
)

// honestRecorder is a fake Slack that keeps every request's headers.
type honestRecorder struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []honestSeen
}

type honestSeen struct {
	what   string // request kind, from the path
	header http.Header
}

func newHonestRecorder(t *testing.T) *honestRecorder {
	h := &honestRecorder{}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		what := r.URL.Path
		if r.Header.Get("Upgrade") != "" {
			what = "websocket handshake"
		}
		h.mu.Lock()
		h.seen = append(h.seen, honestSeen{what: what, header: r.Header.Clone()})
		h.mu.Unlock()
		switch {
		case what == "websocket handshake":
			http.Error(w, "no", http.StatusForbidden) // the handshake's headers are all the test needs
		case r.URL.Path == "/files-pri/T1-F1/redirect.png":
			http.Redirect(w, r, "/files-pri/T1-F1/x.png", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/files-pri/"), strings.HasSuffix(r.URL.Path, ".png"):
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("png"))
		case r.URL.Path == "/upload":
			w.Write([]byte("ok"))
		default:
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *honestRecorder) take() []honestSeen {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.seen
	h.seen = nil
	return s
}

// checkHonest fails t for any request that claims to be a browser: a
// User-Agent other than Clack's (or holding a browser token), or any
// client hint, fetch-metadata, Origin or Referer header.
func checkHonest(t *testing.T, kind string, seen []honestSeen) {
	t.Helper()
	for _, s := range seen {
		uas := s.header.Values("User-Agent")
		if len(uas) != 1 || uas[0] != UserAgent() {
			t.Errorf("%s %s: User-Agent %q, want exactly %q", kind, s.what, uas, UserAgent())
		}
		for _, ua := range uas {
			if tok := browserToken(ua); tok != "" {
				t.Errorf("%s %s: User-Agent %q contains %q", kind, s.what, ua, tok)
			}
		}
		for k := range s.header {
			if BrowserOnlyHeader(k) {
				t.Errorf("%s %s: sent %s: %q", kind, s.what, k, s.header.Values(k))
			}
		}
	}
}

// Every kind of request Clack makes to Slack, for a pasted session and a
// desktop session: Web API (team and org host, form and multipart),
// edgeapi, file download (and its redirect hop), upload, the realtime
// handshake, a request a caller decorated with browser headers, the
// credential-free CDN fetch and desktop sign-in (exchange, token page,
// auth.test). Not one may carry a browser identity.
func TestNoBrowserDisguise(t *testing.T) {
	ctx := context.Background()
	h := newHonestRecorder(t)
	base := h.srv.URL
	pasted := Creds{Token: "xoxc-1", Cookie: "xoxd-1", Team: "T1", TeamURL: base + "/",
		EnterpriseToken: "xoxc-org", EnterpriseID: "E1", EnterpriseURL: base + "/",
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36"} // legacy, ignored
	desktop := desktopCreds(base)
	desktop.EnterpriseToken, desktop.EnterpriseID, desktop.EnterpriseURL = "xoxc-org", "E1", base+"/"
	want := []string{
		"/api/plain.method", "/api/files.upload", "/api/edge/E1/users/info",
		"/files-pri/T1-F1/redirect.png", "/files-pri/T1-F1/x.png", "/upload",
		"websocket handshake", "/api/decorated",
	}
	sort.Strings(want)
	for _, kc := range []struct {
		kind  string
		creds Creds
	}{{"pasted", pasted}, {"desktop", desktop}} {
		c := New(kc.creds)
		if err := c.callOn(ctx, "plain.method", false, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := c.callOn(ctx, "plain.method", true, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := c.CallMultipart(ctx, "files.upload", nil, "file", "x.txt", []byte("x"), nil); err != nil {
			t.Fatal(err)
		}
		if err := c.Edge(ctx, "users/info", map[string]any{"ids": []string{"U1"}}, nil); err != nil {
			t.Fatal(err)
		}
		resp, err := c.Download(ctx, base+"/files-pri/T1-F1/redirect.png")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if err := c.UploadBytes(ctx, base+"/upload", "x.txt", []byte("x"), nil); err != nil {
			t.Fatal(err)
		}
		r := c.NewRTM()
		r.reconnectURL = "ws" + strings.TrimPrefix(base, "http") + "/websocket"
		if err := r.once(ctx); err == nil {
			t.Fatal("handshake should fail against the fake")
		}
		// A request someone dressed up as Chrome still leaves as Clack.
		req, _ := http.NewRequest(http.MethodPost, base+"/api/decorated", nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36")
		req.Header.Set("sec-ch-ua", `"Chromium";v="157"`)
		req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Origin", "https://app.slack.com")
		req.Header.Set("Referer", "https://app.slack.com/client/T1")
		resp, err = c.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if req.Header.Get("Origin") == "" {
			t.Error("the transport changed the caller's request instead of a copy")
		}

		seen := h.take()
		var got []string
		have := map[string]bool{}
		for _, s := range seen {
			if !have[s.what] {
				have[s.what] = true
				got = append(got, s.what)
			}
		}
		sort.Strings(got)
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: request kinds\n got %v\nwant %v", kc.kind, got, want)
		}
		checkHonest(t, kc.kind, seen)
	}

	// Credential-free CDN and image fetches (PublicHTTP), for either kind.
	ht, ok := PublicHTTP.Transport.(honestTransport)
	if !ok || ht.base != http.RoundTripper(publicTransport) {
		t.Fatalf("PublicHTTP.Transport = %T, want honestTransport around publicTransport", PublicHTTP.Transport)
	}
	defer func(rt http.RoundTripper) { PublicHTTP.Transport = rt }(PublicHTTP.Transport)
	PublicHTTP.Transport = honestTransport{rewriteTransport{h.srv}} // publicTransport won't dial loopback
	if _, _, err := GetNoCreds(ctx, "https://a.slack-edge.com/avatar.png", 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, _, err := FetchPublic(ctx, "https://example.com/picture.png", 1<<20); err != nil {
		t.Fatal(err)
	}
	if seen := h.take(); len(seen) != 2 {
		t.Errorf("public fetches: %d requests, want 2", len(seen))
	} else {
		checkHonest(t, "public", seen)
	}
}

// Desktop sign-in: the magic-link exchange, the token page and auth.test.
func TestNoBrowserDisguiseSignIn(t *testing.T) {
	f := newFakeDesktopSlack(t)
	sets, _ := ParseMagicLink("slack://t08psq7bq/magic-login/abc")
	if _, err := LoginMagic(context.Background(), sets, MagicConfig{}); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 3 {
		t.Fatalf("%d requests, want exchange, token page, auth.test", len(reqs))
	}
	var seen []honestSeen
	for _, r := range reqs {
		u := url.URL{Path: r.URL.Path}
		seen = append(seen, honestSeen{what: u.String(), header: r.Header})
	}
	checkHonest(t, "sign-in", seen)
}
