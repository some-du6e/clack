package slack

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Event is one realtime event; Raw holds the full payload.
type Event struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype,omitempty"`
	Channel json.RawMessage `json:"channel,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

// RTM maintains a reconnecting connection to the realtime gateway.
type RTM struct {
	c       *Client
	Events  chan Event
	OnState func(connected bool)

	mu   sync.Mutex
	conn *websocket.Conn
	next atomic.Int64
	// reconnectURL is the gateway's last reconnect_url and reconnectAt
	// when it arrived (Slack's _fastReconnectUrl and
	// _fastReconnectUrlReceivedAtTime)
	reconnectURL string
	reconnectAt  ClockReading
	fails        int
	// kick cuts a backoff wait short (Reconnect)
	kick chan struct{}
	// fast: the current connection came from the gateway's reconnect_url
	// (Slack's FAST_RECONNECTING; any other connect is a slow one)
	fast atomic.Bool

	// OnHello runs after every (re)connect, e.g. to resend subscriptions.
	OnHello func()
}

// LastConnectFast reports whether the current (or last) connection was a
// fast reconnect: dialled at the reconnect_url the gateway handed out,
// where Slack's socket manager is FAST_RECONNECTING. Slack counts every
// other connect in slowConnectionCount.
func (r *RTM) LastConnectFast() bool { return r.fast.Load() }

func (c *Client) NewRTM() *RTM {
	return &RTM{c: c, Events: make(chan Event, 1024), kick: make(chan struct{}, 1)}
}

// MaxRTMFails is how many failed connects in a row (none reached hello)
// Run takes at the normal backoff cap; after that it waits
// RTMBackoffLong between tries. It never gives up: like Slack's socket
// manager it keeps trying for as long as live updates are on, and core
// polls in the meantime.
const MaxRTMFails = 8

// Run's backoff: rtmBackoffBase doubling up to rtmBackoffCap, then
// rtmBackoffLong once MaxRTMFails connects in a row failed (Slack's
// WAIT_FOR_RATE_LIMIT backs off from 1 s to a 60 s cap). Vars for tests.
var (
	rtmBackoffBase = time.Second
	rtmBackoffCap  = 30 * time.Second
	rtmBackoffLong = 60 * time.Second
)

// ReconnectURLMaxAge is how long a reconnect_url stays usable. Slack's
// socket manager (getFastReconnectUrl) drops it once Date.now() is more
// than 285 s past its arrival, and connects with a fresh
// client.getWebSocketURL instead. The age is wall-clock, so time asleep
// counts.
const ReconnectURLMaxAge = 285 * time.Second

// errStaleURL: the gateway refused the reconnect_url (Slack's
// clearFastReconnectUrl cases). Run connects again at once with a fresh
// URL and doesn't count a failure.
var errStaleURL = errors.New("rtm: reconnect_url refused")

// session is how one connect went.
type session struct {
	hello bool          // the gateway said hello: the connect worked
	up    time.Duration // wall-clock time from the dial to the drop (0 if the dial failed)
	err   error
}

// Run blocks until ctx is done or the session ended, reconnecting with
// exponential backoff.
//
// A connect counts as failed only if it never got hello. Slack's socket
// manager does the same: CONNECTED clears its error count and its backoff.
// Run used to judge a connection by how long it lasted on Go's monotonic
// clock, which stops while a Mac sleeps, so an hour-long connection that
// slept through most of it counted as a failed connect, and after eight of
// those Run gave up on live updates for good.
func (r *RTM) Run(ctx context.Context) {
	select { // a Reconnect from before this Run isn't for it
	case <-r.kick:
	default:
	}
	backoff := rtmBackoffBase
	for ctx.Err() == nil && !r.c.SignedOut() {
		s := r.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		if r.OnState != nil {
			r.OnState(false)
		}
		if r.c.SignedOut() {
			log.Printf("rtm: session ended, not reconnecting")
			return
		}
		stale := errors.Is(s.err, errStaleURL)
		wait := backoff
		switch {
		case stale:
			wait = 0 // not the network's fault: straight back with a fresh URL
		case s.hello:
			r.fails = 0
			backoff = rtmBackoffBase
			wait = backoff
		default:
			r.fails++
		}
		if rl, ok := s.err.(rateLimitedErr); ok {
			wait = rl.wait
		}
		switch {
		case stale:
			log.Printf("rtm: gateway refused the reconnect_url, connecting with a fresh URL")
		case s.hello:
			log.Printf("rtm: disconnected (%v) after %s (fast=%v), retrying in %s", s.err, s.up.Round(time.Second), r.fast.Load(), wait)
			if connReset(s.err) {
				r.c.ResetConns() // the path that reset the socket reset the pooled API connections too
			}
		case r.fails == MaxRTMFails:
			log.Printf("rtm: connect failed (%v); %d failed connects in a row, retrying every %s until one works", s.err, r.fails, rtmBackoffLong)
		default:
			log.Printf("rtm: connect failed (%v), retrying in %s", s.err, wait)
		}
		select {
		case <-ctx.Done():
			return
		case <-r.kick:
		case <-time.After(wait):
		}
		if stale {
			continue
		}
		backoff *= 2
		limit := rtmBackoffCap
		if r.fails >= MaxRTMFails {
			limit = rtmBackoffLong
		}
		if backoff > limit {
			backoff = limit
		}
	}
}

// Reconnect drops the socket and connects again at once with a fresh
// client.getWebSocketURL (not the reconnect_url, which belongs to the
// path that just went away). Core calls it when the machine wakes or its
// network addresses change, as Chromium's network change notifier makes
// Slack's client do. A no-op apart from forgetting the reconnect_url while
// Run isn't running.
func (r *RTM) Reconnect(why string) {
	r.mu.Lock()
	conn := r.conn
	r.reconnectURL = ""
	r.mu.Unlock()
	if conn != nil {
		log.Printf("rtm: %s, reconnecting", why)
		conn.CloseNow()
	}
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// takeReconnectURL returns the reconnect_url to dial, if it is fresh
// enough, and forgets it either way: each one is tried once.
func (r *RTM) takeReconnectURL() string {
	r.mu.Lock()
	u, at := r.reconnectURL, r.reconnectAt
	r.reconnectURL = ""
	r.mu.Unlock()
	if u == "" {
		return ""
	}
	if age := ReadClock().WallSince(at); age > ReconnectURLMaxAge || age < 0 {
		log.Printf("rtm: reconnect_url is %s old, connecting with a fresh URL", age.Round(time.Second))
		return ""
	}
	return u
}

// rateLimitedErr: an error frame said to back off for wait.
type rateLimitedErr struct {
	msg  string
	wait time.Duration
}

func (e rateLimitedErr) Error() string { return "rtm: " + e.msg }

// once connects and reads until the socket drops.
func (r *RTM) once(ctx context.Context) error { return r.connect(ctx).err }

func (r *RTM) connect(ctx context.Context) (s session) {
	u := r.takeReconnectURL()
	fast := u != ""
	r.fast.Store(fast)
	if u == "" {
		u = r.c.WebSocketURL(ctx, r.fails%3 == 2) // every third failure, try the backup gateway
	}
	// The URL carries the token and the dial sends the cookie: it must be
	// on slack.com, whatever client.getWebSocketURL or a reconnect_url said.
	if pu, err := url.Parse(u); err != nil || !r.c.credsOK(pu) {
		s.err = ErrUntrustedHost
		return s
	}
	if r.c.SignedOut() {
		s.err = ErrSignedOut
		return s
	}
	t0 := time.Now()
	h := http.Header{}
	h.Set("Cookie", r.c.cookieHeader())
	// Clack's own User-Agent and no Origin: Origin is how a server tells
	// which web page opened a socket, so only browsers send it. Nothing in
	// this code or Slack's docs says the gateway needs one, and Slack's SDKs
	// dial it without one. If it ever did, the dial would fail (a refused
	// handshake), which is far cheaper than a disguise Grid signs out for.
	// No browser Pragma/Cache-Control either (ua.go).
	HonestHeaders(h)
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, _, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPHeader: h, HTTPClient: r.c.HTTP, CompressionMode: websocket.CompressionContextTakeover})
	cancel()
	if err != nil {
		s.err = scrubErr(err) // the dial error quotes the URL, token included
		return s
	}
	upAt := ReadClock()
	conn.SetReadLimit(64 << 20)
	r.mu.Lock()
	r.conn = conn
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.conn = nil
		r.mu.Unlock()
		conn.CloseNow()
		s.up = ReadClock().WallSince(upAt)
	}()

	cctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-cctx.Done():
				return
			case <-t.C:
				_ = r.Send(map[string]any{"type": "ping"})
			}
		}
	}()

	for {
		_, data, err := conn.Read(cctx)
		if err != nil {
			s.err = err
			return s
		}
		var ev Event
		if json.Unmarshal(data, &ev) != nil {
			continue
		}
		ev.Raw = data
		switch ev.Type {
		case "hello":
			s.hello = true
			log.Printf("rtm: connected in %s", time.Since(t0).Round(time.Millisecond))
			if r.OnState != nil {
				r.OnState(true)
			}
			if r.OnHello != nil {
				go r.OnHello()
			}
		case "error":
			s.err = r.errorFrame(data, fast)
			return s
		case "reconnect_url":
			var x struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(data, &x)
			if r.c.gatewayURLOK(x.URL) { // it carries the token: Slack hosts only
				at := ReadClock()
				r.mu.Lock()
				r.reconnectURL, r.reconnectAt = x.URL, at
				r.mu.Unlock()
			}
			continue
		case "pong":
			continue
		case "":
			// reply to one of our frames; surface failures only
			var rep struct {
				OK    *bool `json:"ok"`
				Error struct {
					Msg string `json:"msg"`
				} `json:"error"`
				ReplyTo int64 `json:"reply_to"`
			}
			if json.Unmarshal(data, &rep) == nil && rep.OK != nil && !*rep.OK {
				log.Printf("rtm: frame %d rejected: %s", rep.ReplyTo, rep.Error.Msg)
			}
			continue
		case "goodbye":
			return s
		}
		select {
		case r.Events <- ev:
		default:
			log.Printf("rtm: event buffer full, dropping %s", ev.Type)
		}
	}
}

// errorFrame handles the gateway's "error" frame the way Slack's socket
// manager (_handleSocketErrorMessage) does: every one closes the socket.
// "Socket URL has expired" and invalid_auth on a fast reconnect only
// mean the reconnect_url is no good, so the next connect is a slow one at
// once (errStaleURL); on a slow connect they are ordinary failures. A rate limit waits its retry_after (30 s without
// one). Any other auth code ends the session.
func (r *RTM) errorFrame(data []byte, fast bool) error {
	var fe struct {
		Error struct {
			Code       any    `json:"code"`
			Msg        string `json:"msg"`
			RetryAfter int    `json:"retry_after"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &fe)
	msg := fe.Error.Msg
	log.Printf("rtm: gateway error frame: %s", msg)
	switch msg {
	case "Socket URL has expired", "Subscription failure from the CS":
		r.mu.Lock()
		r.reconnectURL = ""
		r.mu.Unlock()
		if msg == "Socket URL has expired" && fast {
			return errStaleURL // a fresh URL can't loop here: the next one is fresh
		}
		return errors.New("rtm: " + msg)
	case "rate_limited", "ratelimited", "Ratelimited", "backend_overloaded", "unknown":
		wait := 30 * time.Second
		if fe.Error.RetryAfter > 0 {
			wait = time.Duration(fe.Error.RetryAfter) * time.Second
		}
		return rateLimitedErr{msg: msg, wait: wait}
	}
	if fast && strings.Contains(msg, "invalid_auth") {
		// Slack: "Got invalid_auth when trying to fast reconnect; clearing
		// fast reconnect URL and trying slow reconnection instead". A
		// session that really ended fails client.getWebSocketURL next.
		return errStaleURL
	}
	for code := range authCodes {
		if strings.Contains(msg, code) {
			r.c.markAuth(code)
			return errors.New("rtm: " + code)
		}
	}
	return errors.New("rtm: " + msg)
}

// Send writes a JSON frame (assigning an id if absent).
func (r *RTM) Send(v map[string]any) error {
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()
	if conn == nil {
		return websocket.CloseError{Code: websocket.StatusGoingAway}
	}
	if _, ok := v["id"]; !ok {
		v["id"] = r.next.Add(1)
	}
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, b)
}

// Typing sends a typing indicator for a channel (optionally a thread).
func (r *RTM) Typing(channel, threadTS string) error {
	m := map[string]any{"type": "typing", "channel": channel}
	if threadTS != "" {
		m["thread_ts"] = threadTS
	}
	return r.Send(m)
}

// SubscribePresence asks the gateway for presence updates of the given
// users. It replaces the previous subscription set (as the web client does).
func (r *RTM) SubscribePresence(ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	return r.Send(map[string]any{"type": "presence_sub", "ids": ids})
}

// SubscribeUsers asks for user_change events for the given users (flannel
// only pushes profile changes for users the client has subscribed to).
// MaxUserSubs is the gateway's limit for one user_change subscription
// (verified: 100 is accepted, 101 is "Too many updated ids").
const MaxUserSubs = 100

func (r *RTM) SubscribeUsers(ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	if len(ids) > MaxUserSubs {
		ids = ids[:MaxUserSubs]
	}
	return r.Send(map[string]any{"type": "subscription_update", "subtype": "user_change_subscribe_request", "ids": ids})
}

// Connected reports whether the socket is currently up.
func (r *RTM) Connected() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conn != nil
}
