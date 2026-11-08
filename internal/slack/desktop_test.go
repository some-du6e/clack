package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// sessionServer is a Slack API that rotates d-s on every answer, the way
// Slack does, and records what each request carried.
type sessionServer struct {
	srv *httptest.Server
	mu  sync.Mutex
	// per request
	cookies, uas []string
	hints        []string
	ds           int
	// extra Set-Cookies for the next answer
	next []*http.Cookie
}

func newSessionServer(t *testing.T) *sessionServer {
	s := &sessionServer{ds: 1794000000}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.cookies = append(s.cookies, r.Header.Get("Cookie"))
		s.uas = append(s.uas, r.Header.Get("User-Agent"))
		s.hints = append(s.hints, r.Header.Get("sec-ch-ua"))
		s.ds++
		http.SetCookie(w, &http.Cookie{Name: "d-s", Value: strconv.Itoa(s.ds), Domain: ".slack.com", Path: "/", Secure: true, HttpOnly: true})
		for _, k := range s.next {
			http.SetCookie(w, k)
		}
		s.next = nil
		s.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/files-pri/") {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("png"))
			return
		}
		if r.URL.Path == "/upload" {
			w.Write([]byte("ok"))
			return
		}
		if r.Header.Get("Upgrade") != "" {
			http.Error(w, "no", http.StatusForbidden) // the handshake's headers are all the test needs
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *sessionServer) last() (cookie, ua, hints string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.cookies) - 1
	return s.cookies[n], s.uas[n], s.hints[n]
}

func (s *sessionServer) setNext(cs ...*http.Cookie) {
	s.mu.Lock()
	s.next = cs
	s.mu.Unlock()
}

// every way a client talks to Slack: an API call, a file download, an
// upload and the realtime socket's handshake
func exerciseClient(t *testing.T, c *Client, s *sessionServer) (cookies, uas, hints []string) {
	t.Helper()
	ctx := context.Background()
	add := func() {
		ck, ua, h := s.last()
		cookies, uas, hints = append(cookies, ck), append(uas, ua), append(hints, h)
	}
	if err := c.Call(ctx, "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	add()
	resp, err := c.Download(ctx, s.srv.URL+"/files-pri/T1-F1/x.png")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	add()
	if err := c.UploadBytes(ctx, s.srv.URL+"/upload", "x.txt", []byte("x"), nil); err != nil {
		t.Fatal(err)
	}
	add()
	r := c.NewRTM()
	r.reconnectURL = "ws" + strings.TrimPrefix(s.srv.URL, "http") + "/websocket"
	if err := r.once(ctx); err == nil {
		t.Fatal("handshake should fail against the fake")
	}
	add()
	return
}

// A pasted session is exactly as before: d only (never d-s, whatever Slack
// sets), the browser's user agent and client hints, Set-Cookie ignored,
// nothing written anywhere.
func TestPastedSessionUnchanged(t *testing.T) {
	s := newSessionServer(t)
	ua := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-paste%2F1", CookieDS: "1700000000", TeamURL: s.srv.URL + "/", UserAgent: ua})
	if _, ok := c.HTTP.Transport.(*http.Transport); !ok {
		t.Errorf("pasted session transport %T, want the plain one", c.HTTP.Transport)
	}
	saved := false
	c.PersistSession(func(Creds) error { saved = true; return nil })
	s.setNext(&http.Cookie{Name: "d", Value: "xoxd-rotated", Domain: ".slack.com"}, &http.Cookie{Name: "lc", Value: "1"})
	for round := 0; round < 2; round++ {
		cookies, uas, hints := exerciseClient(t, c, s)
		for i := range cookies {
			if cookies[i] != "d=xoxd-paste%2F1" {
				t.Errorf("round %d request %d Cookie = %q, want d only", round, i, cookies[i])
			}
			if uas[i] != ua {
				t.Errorf("round %d request %d UA = %q", round, i, uas[i])
			}
		}
		// client hints on https requests, none on the socket (as before)
		if hints[0] == "" || hints[1] == "" || hints[2] == "" || hints[3] != "" {
			t.Errorf("client hints %q", hints)
		}
	}
	c.FlushSession()
	if saved || c.SessionCookie() != "xoxd-paste%2F1" || c.SessionCreds() != c.Creds {
		t.Errorf("pasted session changed: saved %v cookie %q", saved, c.SessionCookie())
	}
}

func desktopCreds(base string) Creds {
	return Creds{Session: SessionDesktop, Token: "xoxc-1", Cookie: "xoxd-mine%2F1", Cookies: "lc=1794000000; d-s=1794000000",
		Team: "T1", UserID: "U1", TeamURL: base + "/"}
}

// A desktop session sends every cookie it has, follows Slack's rotation on
// every kind of request, and presents as Slack desktop everywhere.
func TestDesktopSessionJar(t *testing.T) {
	s := newSessionServer(t)
	c := New(desktopCreds(s.srv.URL))
	cookies, uas, hints := exerciseClient(t, c, s)
	want := []string{
		"d=xoxd-mine%2F1; d-s=1794000000; lc=1794000000",
		"d=xoxd-mine%2F1; d-s=1794000001; lc=1794000000", // d-s rotated by the call
		"d=xoxd-mine%2F1; d-s=1794000002; lc=1794000000", // … and by the download
		"d=xoxd-mine%2F1; d-s=1794000003; lc=1794000000", // … and by the upload
	}
	for i := range want {
		if cookies[i] != want[i] {
			t.Errorf("request %d Cookie = %q, want %q", i, cookies[i], want[i])
		}
		if uas[i] != DesktopUserAgent() {
			t.Errorf("request %d UA = %q, want Slack desktop's", i, uas[i])
		}
		if hints[i] != "" {
			t.Errorf("request %d sent client hints %q", i, hints[i])
		}
	}
	// The socket handshake's answer rotated d-s too.
	if got := c.SessionCreds().Cookies; got != "d-s=1794000004; lc=1794000000" {
		t.Errorf("after the handshake: %q", got)
	}

	// New cookies are kept, deleted ones dropped, a rotated d replaces d,
	// and a deleted d is ignored.
	s.setNext(&http.Cookie{Name: "uc", Value: "u1", Domain: ".slack.com"},
		&http.Cookie{Name: "lc", Value: "", MaxAge: -1},
		&http.Cookie{Name: "d", Value: "xoxd-rotated%2F2", Domain: ".slack.com"})
	if err := c.Call(context.Background(), "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	s.setNext(&http.Cookie{Name: "d", Value: "deleted", Expires: time.Unix(1, 0)},
		&http.Cookie{Name: "bad", Value: strings.Repeat("x", maxCookieValue+1)})
	if err := c.Call(context.Background(), "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(context.Background(), "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	if ck, _, _ := s.last(); ck != "d=xoxd-rotated%2F2; d-s=1794000006; uc=u1" {
		t.Errorf("after rotation Cookie = %q", ck)
	}
	got := c.SessionCreds()
	if got.Cookie != "xoxd-rotated%2F2" || got.Cookies != "d-s=1794000007; uc=u1" || got.Token != "xoxc-1" || !got.Desktop() {
		t.Errorf("SessionCreds = cookie %q others %q", got.Cookie, got.Cookies)
	}
}

// Cookies from anywhere but slack.com (or the client's own API origin, here
// the test server) are never taken in.
func TestDesktopSessionIgnoresOtherHosts(t *testing.T) {
	c := New(desktopCreds("https://hackclub.slack.com"))
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "d", Value: "xoxd-evil"})
		http.SetCookie(w, &http.Cookie{Name: "evil", Value: "1"})
	}))
	defer other.Close()
	req, _ := http.NewRequest(http.MethodGet, other.URL+"/", nil)
	resp, err := c.HTTP.Transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := c.SessionCreds(); got.Cookie != "xoxd-mine%2F1" || strings.Contains(got.Cookies, "evil") {
		t.Errorf("took cookies from another host: %q %q", got.Cookie, got.Cookies)
	}
}

// Rotated cookies reach creds.json, owner-only, debounced: a burst of
// answers is one write.
func TestDesktopSessionPersists(t *testing.T) {
	oldSoon, oldLazy := credsFlushSoon, credsFlushLazy
	credsFlushSoon, credsFlushLazy = 30*time.Millisecond, 150*time.Millisecond
	defer func() { credsFlushSoon, credsFlushLazy = oldSoon, oldLazy }()
	s := newSessionServer(t)
	dir := filepath.Join(t.TempDir(), "clack")
	path := filepath.Join(dir, "creds.json")
	c := New(desktopCreds(s.srv.URL))
	var mu sync.Mutex
	writes := 0
	c.PersistSession(func(nc Creds) error {
		mu.Lock()
		writes++
		mu.Unlock()
		return WriteCreds(path, nc)
	})
	for i := 0; i < 5; i++ {
		if err := c.Call(context.Background(), "plain.method", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	// only d-s moved: the lazy debounce
	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	if writes != 0 {
		t.Errorf("%d writes before the lazy debounce", writes)
	}
	mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := writes
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err := readCredsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Desktop() || got.Cookie != "xoxd-mine%2F1" || got.Cookies != "d-s=1794000005; lc=1794000000" || got.Token != "xoxc-1" {
		t.Errorf("saved %q %q", got.Cookie, got.Cookies)
	}
	mu.Lock()
	if writes != 1 {
		t.Errorf("%d writes for one burst, want 1", writes)
	}
	mu.Unlock()
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("creds.json mode %v", fi.Mode().Perm())
		}
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
			t.Errorf("config dir mode %v", fi.Mode().Perm())
		}
	}

	// A rotated d is saved soon; FlushSession saves what's pending at once.
	s.setNext(&http.Cookie{Name: "d", Value: "xoxd-rotated%2F2"})
	if err := c.Call(context.Background(), "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	c.FlushSession()
	if got, _ := readCredsFile(path); got.Cookie != "xoxd-rotated%2F2" || got.Cookies != "d-s=1794000006; lc=1794000000" {
		t.Errorf("after flush %q %q", got.Cookie, got.Cookies)
	}
	// Nothing new: no write.
	mu.Lock()
	before := writes
	mu.Unlock()
	c.FlushSession()
	mu.Lock()
	if writes != before {
		t.Error("FlushSession wrote unchanged cookies")
	}
	mu.Unlock()

	// After PersistSession(nil) (sign-out, account switch), nothing is
	// written, however late.
	c.PersistSession(nil)
	if err := c.Call(context.Background(), "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	c.FlushSession()
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	if writes != before {
		t.Error("wrote after PersistSession(nil)")
	}
	mu.Unlock()
}

// readCredsFile is ReadCreds without the host check (the test server is
// http on 127.0.0.1).
func readCredsFile(path string) (Creds, error) {
	var c Creds
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	return c, err
}

func TestSetSessionCookie(t *testing.T) {
	p := New(Creds{Token: "xoxc-1", Cookie: "xoxd-a"})
	p.SetSessionCookie("xoxd-b")
	if p.Creds.Cookie != "xoxd-b" || p.cookieHeader() != "d=xoxd-b" {
		t.Errorf("pasted: %q", p.cookieHeader())
	}
	d := New(desktopCreds("https://hackclub.slack.com"))
	d.SetSessionCookie("xoxd-b")
	if d.SessionCookie() != "xoxd-b" || !strings.HasPrefix(d.cookieHeader(), "d=xoxd-b; ") {
		t.Errorf("desktop: %q", d.cookieHeader())
	}
}

func TestPublicUserAgent(t *testing.T) {
	defer SetPublicUserAgent("")
	var got string
	if publicUserAgent() != UserAgent {
		t.Errorf("default %q", publicUserAgent())
	}
	SetPublicUserAgent(DesktopUserAgent())
	got = publicUserAgent()
	if got != DesktopUserAgent() {
		t.Errorf("desktop %q", got)
	}
	SetPublicUserAgent("")
	if publicUserAgent() != UserAgent {
		t.Error("reset")
	}
}
