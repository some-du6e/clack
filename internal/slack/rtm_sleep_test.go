package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeClock is a ClockReading source a test moves by hand: sleep moves
// only the wall clock (a Mac asleep: mach_absolute_time stops), pass
// moves both.
type fakeClock struct {
	mu   sync.Mutex
	wall time.Time
	mono time.Duration
}

func newFakeClock(t *testing.T) *fakeClock {
	c := &fakeClock{wall: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)}
	t.Cleanup(SetClock(c.read))
	return c
}

func (c *fakeClock) read() ClockReading {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ClockReading{Wall: c.wall, Mono: c.mono}
}

func (c *fakeClock) sleep(d time.Duration) {
	c.mu.Lock()
	c.wall = c.wall.Add(d)
	c.mu.Unlock()
}

func (c *fakeClock) pass(d time.Duration) {
	c.mu.Lock()
	c.wall = c.wall.Add(d)
	c.mono += d
	c.mu.Unlock()
}

// syncBuf collects log output from several goroutines.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func captureLog(t *testing.T) *syncBuf {
	var b syncBuf
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&b)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })
	return &b
}

func fastBackoff(t *testing.T) {
	b, c, l := rtmBackoffBase, rtmBackoffCap, rtmBackoffLong
	rtmBackoffBase, rtmBackoffCap, rtmBackoffLong = 2*time.Millisecond, 8*time.Millisecond, 16*time.Millisecond
	t.Cleanup(func() { rtmBackoffBase, rtmBackoffCap, rtmBackoffLong = b, c, l })
}

// fakeGateway is a local realtime gateway the test scripts one connection
// at a time: client.getWebSocketURL hands out /ws/slow, every accepted
// socket arrives on conns, and refuse turns that many dials away with 503.
type fakeGateway struct {
	srv    *httptest.Server
	conns  chan *gwConn
	urls   atomic.Int32 // client.getWebSocketURL calls
	refuse atomic.Int32
}

type gwConn struct {
	c    *websocket.Conn
	path string
	fast bool // dialled at the reconnect_url the test handed out
	done chan struct{}
}

func newFakeGateway(t *testing.T) (*fakeGateway, *Client) {
	g := &fakeGateway{conns: make(chan *gwConn, 16)}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/client.getWebSocketURL":
			g.urls.Add(1)
			ws := "ws" + strings.TrimPrefix(g.srv.URL, "http") + "/ws/slow"
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "primary_websocket_url": ws, "fallback_websocket_url": ws})
		case strings.HasPrefix(r.URL.Path, "/ws/"):
			if g.refuse.Add(-1) >= 0 {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.CloseNow()
			gc := &gwConn{c: c, path: r.URL.Path, fast: r.URL.Query().Get("fast") == "1", done: make(chan struct{})}
			ctx := c.CloseRead(r.Context()) // answers the close handshake and the client's pings
			g.conns <- gc
			select {
			case <-gc.done:
			case <-ctx.Done():
			}
		default:
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	g.refuse.Store(0)
	t.Cleanup(g.srv.Close)
	return g, New(Creds{Token: "xoxc-test", Team: "T1", TeamURL: g.srv.URL + "/"})
}

func (g *fakeGateway) fastURL() string {
	return "ws" + strings.TrimPrefix(g.srv.URL, "http") + "/ws/fast?fast=1"
}

func (g *fakeGateway) next(t *testing.T) *gwConn {
	t.Helper()
	select {
	case gc := <-g.conns:
		return gc
	case <-time.After(5 * time.Second):
		t.Fatal("the client didn't connect again")
		return nil
	}
}

func (gc *gwConn) send(t *testing.T, frame string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := gc.c.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
		t.Fatalf("gateway write: %v", err)
	}
}

// close hangs up the way the gateway does (a close frame).
func (gc *gwConn) close() {
	_ = gc.c.Close(websocket.StatusGoingAway, "")
	close(gc.done)
}

// sync sends a marker event and waits for it on r.Events: every frame
// sent before it has been handled (frames are handled in order).
func (gc *gwConn) sync(t *testing.T, r *RTM) {
	t.Helper()
	gc.send(t, `{"type":"marker"}`)
	for {
		select {
		case ev := <-r.Events:
			if ev.Type == "marker" {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("marker event never arrived")
		}
	}
}

func runRTM(t *testing.T, r *RTM) (stop func(), done <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	d := make(chan struct{})
	go func() { r.Run(ctx); close(d) }()
	stop = func() {
		cancel()
		select {
		case <-d:
		case <-time.After(5 * time.Second):
			t.Error("Run didn't stop")
		}
	}
	t.Cleanup(stop)
	return stop, d
}

// A connection that lived through a sleep was up for an hour of wall
// clock but a moment of monotonic clock (darwin's mach_absolute_time
// stops while the Mac sleeps). It got hello, so it is no failed connect:
// three times MaxRTMFails of them in a row leave Run connected, with no
// failures counted and the hour logged as the socket's lifetime.
func TestRTMSleepIsNotAFailedConnect(t *testing.T) {
	fastBackoff(t)
	clk := newFakeClock(t)
	logs := captureLog(t)
	g, c := newFakeGateway(t)
	r := c.NewRTM()
	stop, done := runRTM(t, r)
	for i := 0; i < 3*MaxRTMFails; i++ {
		gc := g.next(t)
		gc.send(t, `{"type":"hello"}`)
		gc.sync(t, r)
		clk.sleep(time.Hour)
		gc.close()
		select {
		case <-done:
			t.Fatalf("Run gave up after %d connections that slept", i+1)
		default:
		}
	}
	g.next(t) // and it connects once more
	stop()
	if r.fails != 0 {
		t.Errorf("fails = %d after connections that all got hello, want 0", r.fails)
	}
	if out := logs.String(); !strings.Contains(out, "after 1h0m0s") {
		t.Errorf("disconnect lines don't give the wall-clock lifetime:\n%s", out)
	}
	if strings.Contains(logs.String(), "connect failed") {
		t.Errorf("a connection that got hello was logged as a failed connect:\n%s", logs.String())
	}
}

// Connects that never get hello count as failures, but Run never gives
// up: past MaxRTMFails it keeps trying (slower) until one works.
func TestRTMNeverGivesUp(t *testing.T) {
	fastBackoff(t)
	newFakeClock(t)
	logs := captureLog(t)
	g, c := newFakeGateway(t)
	g.refuse.Store(3 * MaxRTMFails)
	up := make(chan bool, 64)
	r := c.NewRTM()
	r.OnState = func(on bool) { up <- on }
	runRTM(t, r)
	gc := g.next(t)
	gc.send(t, `{"type":"hello"}`)
	deadline := time.After(5 * time.Second)
	for on := false; !on; {
		select {
		case on = <-up:
		case <-deadline:
			t.Fatal("never connected after the refusals")
		}
	}
	if !strings.Contains(logs.String(), "failed connects in a row, retrying every") {
		t.Errorf("no slow-down line after %d failures:\n%s", MaxRTMFails, logs.String())
	}
}

// A reconnect_url past Slack's 285 s (wall clock, so sleep counts) is not
// dialled: the next connect asks client.getWebSocketURL. A fresh one is.
// "Socket URL has expired" on a fast reconnect closes the socket at once
// (the gateway would keep it open about 5 s) and reconnects slow straight
// away without counting a failure; so does invalid_auth on a fast
// reconnect, which doesn't end the session.
func TestRTMReconnectURLAge(t *testing.T) {
	fastBackoff(t)
	clk := newFakeClock(t)
	logs := captureLog(t)
	g, c := newFakeGateway(t)
	r := c.NewRTM()
	runRTM(t, r)

	gc := g.next(t)
	gc.send(t, `{"type":"hello"}`)
	gc.send(t, `{"type":"reconnect_url","url":"`+g.fastURL()+`"}`)
	gc.sync(t, r)
	clk.pass(ReconnectURLMaxAge + time.Second)
	gc.close()
	if gc = g.next(t); gc.fast {
		t.Fatal("dialled a reconnect_url older than ReconnectURLMaxAge")
	}
	if n := g.urls.Load(); n != 2 {
		t.Errorf("client.getWebSocketURL calls = %d, want 2", n)
	}
	if !strings.Contains(logs.String(), "reconnect_url is 4m46s old") {
		t.Errorf("no stale reconnect_url line:\n%s", logs.String())
	}

	// a sleep counts toward the age even though the monotonic clock stood still
	gc.send(t, `{"type":"hello"}`)
	gc.send(t, `{"type":"reconnect_url","url":"`+g.fastURL()+`"}`)
	gc.sync(t, r)
	clk.sleep(10 * time.Minute)
	gc.close()
	if gc = g.next(t); gc.fast {
		t.Fatal("dialled a reconnect_url from before a 10 minute sleep")
	}

	// fresh: used
	gc.send(t, `{"type":"hello"}`)
	gc.send(t, `{"type":"reconnect_url","url":"`+g.fastURL()+`"}`)
	gc.sync(t, r)
	clk.pass(10 * time.Second)
	gc.close()
	if gc = g.next(t); !gc.fast {
		t.Fatal("a 10 s old reconnect_url wasn't used")
	}
	urls := g.urls.Load()

	// expired on the gateway's side: closed by the client, slow connect next
	gc.send(t, `{"type":"error","error":{"code":1,"msg":"Socket URL has expired"}}`)
	t0 := time.Now()
	if gc = g.next(t); gc.fast {
		t.Fatal("reconnected to the expired URL")
	}
	if d := time.Since(t0); d > 2*time.Second {
		t.Errorf("reconnect after an expired URL took %s: the client waited for the gateway to hang up", d)
	}
	if g.urls.Load() != urls+1 {
		t.Error("no fresh client.getWebSocketURL after the expired URL")
	}

	// invalid_auth on a fast reconnect: a slow one, still signed in
	gc.send(t, `{"type":"hello"}`)
	gc.send(t, `{"type":"reconnect_url","url":"`+g.fastURL()+`"}`)
	gc.sync(t, r)
	gc.close()
	if gc = g.next(t); !gc.fast {
		t.Fatal("fresh reconnect_url not used")
	}
	gc.send(t, `{"type":"error","error":{"code":1,"msg":"invalid_auth"}}`)
	if gc = g.next(t); gc.fast {
		t.Fatal("reconnected to the reconnect_url that answered invalid_auth")
	}
	if c.SignedOut() {
		t.Error("invalid_auth on a fast reconnect signed the session out")
	}
	if strings.Contains(logs.String(), "connect failed") {
		t.Errorf("a refused reconnect_url counted as a failed connect:\n%s", logs.String())
	}
}

// Reconnect drops the socket and connects again at once, on a fresh URL
// even with a reconnect_url in hand.
func TestRTMReconnect(t *testing.T) {
	newFakeClock(t)
	captureLog(t)
	g, c := newFakeGateway(t)
	r := c.NewRTM()
	runRTM(t, r)
	gc := g.next(t)
	gc.send(t, `{"type":"hello"}`)
	gc.send(t, `{"type":"reconnect_url","url":"`+g.fastURL()+`"}`)
	gc.sync(t, r)
	t0 := time.Now()
	r.Reconnect("network changed")
	if gc = g.next(t); gc.fast {
		t.Error("Reconnect dialled the old path's reconnect_url")
	}
	// rtmBackoffBase is the real 1 s here: Reconnect skips the wait
	if d := time.Since(t0); d > 900*time.Millisecond {
		t.Errorf("Reconnect took %s, want no backoff", d)
	}
}

// resetServer answers every API call ok, except that it resets (RST) or
// closes the connection under any request rule picks; nth counts requests
// per connection from 1.
type resetServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	per  map[string]int
	hits map[string]int
	rule func(method string, nth int) (reset, rst bool)
	news atomic.Int32 // connections opened
}

func newResetServer(t *testing.T) (*resetServer, *Client) {
	s := &resetServer{per: map[string]int{}, hits: map[string]int{}}
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		s.mu.Lock()
		s.per[r.RemoteAddr]++
		nth := s.per[r.RemoteAddr]
		s.hits[method]++
		rule := s.rule
		s.mu.Unlock()
		if rule != nil {
			if reset, rst := rule(method, nth); reset {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				if tc, ok := conn.(*net.TCPConn); ok && rst {
					tc.SetLinger(0) // close with an RST: "connection reset by peer"
				}
				conn.Close()
				return
			}
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	s.srv.Config.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateNew {
			s.news.Add(1)
		}
	}
	s.srv.Start()
	t.Cleanup(s.srv.Close)
	return s, New(Creds{Token: "xoxc-test", Team: "T1", TeamURL: s.srv.URL + "/"})
}

func (s *resetServer) set(rule func(method string, nth int) (bool, bool)) {
	s.mu.Lock()
	s.rule = rule
	s.mu.Unlock()
}

func (s *resetServer) hit(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[method]
}

// A read whose pooled connection turns out dead (reset or closed before
// any response) is sent once more on a new connection and succeeds; a
// write on a dead pooled connection is never sent twice; a read that fails
// on a new connection isn't retried either (nothing says the path is
// stale).
func TestDeadPooledConnRetriesReadsOnce(t *testing.T) {
	captureLog(t)
	s, c := newResetServer(t)
	ctx := context.Background()
	for _, rst := range []bool{true, false} {
		c.ResetConns()
		s.set(nil)
		if err := c.Call(ctx, "users.info", url.Values{"user": {"U1"}}, nil); err != nil {
			t.Fatal(err)
		}
		before := s.hit("users.info")
		// the second request on any connection dies
		s.set(func(_ string, nth int) (bool, bool) { return nth == 2, rst })
		if err := c.Call(ctx, "users.info", url.Values{"user": {"U2"}}, nil); err != nil {
			t.Fatalf("rst=%v: read on a dead pooled connection failed: %v", rst, err)
		}
		if n := s.hit("users.info") - before; n != 2 {
			t.Errorf("rst=%v: users.info reached the server %d times, want 2 (the dead one and the retry)", rst, n)
		}
	}

	// a write: the retry's connection has served one request, so this one dies
	s.set(func(_ string, nth int) (bool, bool) { return nth == 2, true })
	err := c.Call(ctx, "chat.postMessage", url.Values{"channel": {"C1"}, "text": {"hi"}}, nil)
	if err == nil || !connReset(err) {
		t.Fatalf("chat.postMessage on a dead connection: err = %v, want the reset", err)
	}
	if n := s.hit("chat.postMessage"); n != 1 {
		t.Errorf("chat.postMessage reached the server %d times, want 1 (writes never retry)", n)
	}

	// a reset on a brand-new connection is the network, not a stale pool
	c.ResetConns()
	s.set(func(m string, _ int) (bool, bool) { return m == "conversations.history", true })
	if err := c.Call(ctx, "conversations.history", url.Values{"channel": {"C1"}}, nil); err == nil {
		t.Fatal("conversations.history succeeded against a server that resets it")
	}
	if n := s.hit("conversations.history"); n != 1 {
		t.Errorf("conversations.history on a new connection sent %d times, want 1", n)
	}
}

func TestReadOnlyMethod(t *testing.T) {
	for _, m := range []string{"users.info", "conversations.history", "conversations.replies", "files.info", "bots.info",
		"messages.list", "bookmarks.list", "users.list", "users.prefs.get", "users.conversations", "edge:users/info", "client.counts"} {
		if !readOnlyMethod(m) {
			t.Errorf("%s not read-only", m)
		}
	}
	for _, m := range []string{"chat.postMessage", "chat.update", "conversations.mark", "reactions.add", "users.prefs.set",
		"users.setPresence", "users.profile.set", "users.channelSections.create", "files.upload", "users.emailtoken.get"} {
		if readOnlyMethod(m) {
			t.Errorf("%s counted read-only: a retry could do it twice", m)
		}
	}
}

// ResetConns empties the API pool through honestTransport (and
// desktopTransport), so the next call dials a new connection.
func TestResetConnsClosesIdle(t *testing.T) {
	s, c := newResetServer(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := c.Call(ctx, "users.info", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := s.news.Load(); n != 1 {
		t.Fatalf("connections = %d before ResetConns, want 1 (keep-alive)", n)
	}
	c.ResetConns()
	if err := c.Call(ctx, "users.info", nil, nil); err != nil {
		t.Fatal(err)
	}
	if n := s.news.Load(); n != 2 {
		t.Errorf("connections = %d after ResetConns, want 2", n)
	}
	var closed atomic.Int32
	(&desktopTransport{base: closeCounter{&closed}}).CloseIdleConnections()
	if closed.Load() != 1 {
		t.Error("desktopTransport doesn't forward CloseIdleConnections")
	}
}

type closeCounter struct{ n *atomic.Int32 }

func (closeCounter) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrNotSupported
}
func (c closeCounter) CloseIdleConnections() { c.n.Add(1) }
