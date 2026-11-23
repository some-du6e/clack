package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/some-du6e/clack/internal/stackdump"
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
	// activityAt is the socket manager's lastSocketActivity: when a frame
	// that counts as the viewer's activity last went out (activityFrames),
	// on the monotonic clock since rtmEpoch, +1 (0: none yet)
	activityAt atomic.Int64
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

	// waiters are the frames Request sent on the current connection,
	// by id, waiting for the gateway's reply (r.mu)
	waiters map[int64]chan json.RawMessage
	// helloCh closes when the current connection says hello (helloOK) or
	// ends without one; Request waits for it, as Slack's sendMessage waits
	// for the socket to be connected (r.mu)
	helloCh chan struct{}
	helloOK bool

	// OnHello runs after every (re)connect, e.g. to resend subscriptions.
	OnHello func()
	// OnDrop sees an event Events had no room for, on the reader
	// goroutine, before it's gone (core notes a dropped message's ts).
	OnDrop func(Event)

	// dumps keeps the watchdog's goroutine dumps to one per 10 minutes
	dumps stackdump.Limiter
}

// LastConnectFast reports whether the current (or last) connection was a
// fast reconnect: dialled at the reconnect_url the gateway handed out,
// where Slack's socket manager is FAST_RECONNECTING. Slack counts every
// other connect in slowConnectionCount.
func (r *RTM) LastConnectFast() bool { return r.fast.Load() }

func (c *Client) NewRTM() *RTM {
	return &RTM{c: c, Events: make(chan Event, 1024), kick: make(chan struct{}, 1), dumps: stackdump.Limiter{Every: 10 * time.Minute}}
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

// The socket's liveness check, Slack's SlackSocket (client-boot Kot3
// _onSocketOpen): a ping every rtmPingEvery, and once rtmPongTimeout has
// passed since the socket opened or a pong last answered one, the socket
// closes with 4998 "Too long since we received a pong" and Run connects
// again. Slack's clock there is Date.now(), so a sleep counts. A socket
// that goes quiet without a close frame (a sleep, a NAT mapping or a
// Wi-Fi network that went away, a half-open TCP connection) otherwise
// blocks Read for as long as the kernel keeps the connection, and every
// event the gateway sends meanwhile is lost with nobody noticing: Karim's
// #clack drew nothing from 00:54Z to 03:53Z on 2026-11-20. Vars for
// tests.
var rtmPingEvery, rtmPongTimeout atomic.Int64 // nanoseconds

// The socket's watchdog, beside the liveness check: a socket whose pongs
// come can still be dead to Slack's events (Karim's α.136 on 2026-11-22:
// nothing at all from 01:57Z to 02:42Z, the pongs answered and no line
// logged, a reply's toast lost). A healthy gateway sends something of its
// own accord well within ReconnectURLMaxAge (a reconnect_url at the
// least), so with no such frame for rtmFrameTimeout, twice that, while
// the pongs come, or with the pong check not run for rtmTickTimeout or
// three of its intervals (a ping stuck in its write, so the check that
// would close the socket never comes), the watchdog logs it with every
// goroutine's stack and closes the socket with 4998, and Run connects
// again as after the liveness check (the reconnect_url is stale by then:
// a slow connect, as Slack's socket manager makes it). It looks every
// rtmWatchEvery; a look that comes long after the last (the machine
// slept) leaves the socket to the pong check. Vars for tests.
var rtmFrameTimeout, rtmTickTimeout, rtmWatchEvery atomic.Int64 // nanoseconds

// rtmPingHook, for tests, runs at each tick of the pong check, before it
// looks (one that blocks stalls the check).
var rtmPingHook atomic.Pointer[func(ctx context.Context)]

func init() {
	rtmPingEvery.Store(int64(10 * time.Second))
	rtmPongTimeout.Store(int64(90 * time.Second))
	rtmFrameTimeout.Store(int64(2 * ReconnectURLMaxAge))
	rtmTickTimeout.Store(int64(30 * time.Second)) // three of the pong check's 10 s
	rtmWatchEvery.Store(int64(10 * time.Second))
}

// SetStallTimers sets the watchdog's timeouts, for a socket with no frame
// of Slack's own (2 × ReconnectURLMaxAge) and for a pong check that
// hasn't run (30 s), and how often it looks (10 s), and returns the
// undo: for tests.
func SetStallTimers(frames, ticks, every time.Duration) (restore func()) {
	f, k, e := rtmFrameTimeout.Swap(int64(frames)), rtmTickTimeout.Swap(int64(ticks)), rtmWatchEvery.Swap(int64(every))
	return func() { rtmFrameTimeout.Store(f); rtmTickTimeout.Store(k); rtmWatchEvery.Store(e) }
}

// SetPingTimers sets the liveness check's ping interval and pong timeout
// (Slack's 10 s and 90 s) and returns the undo: for tests, which can't
// wait 90 s for a socket that went quiet.
func SetPingTimers(every, timeout time.Duration) (restore func()) {
	e, t := rtmPingEvery.Swap(int64(every)), rtmPongTimeout.Swap(int64(timeout))
	return func() { rtmPingEvery.Store(e); rtmPongTimeout.Store(t) }
}

// pongTimeout is the close reason Slack's socket gives (status 4998).
const pongTimeout = "Too long since we received a pong"

// errPongTimeout: the socket was closed by the liveness check.
var errPongTimeout = errors.New("rtm: " + pongTimeout)

// The watchdog's close reasons (status 4998 too) and errors.
const (
	noFrames  = "Too long since we received an event"
	pingStall = "Too long since the pong check ran"
)

var (
	errNoFrames  = errors.New("rtm: " + noFrames)
	errPingStall = errors.New("rtm: " + pingStall)
)

// session is how one connect went.
type session struct {
	hello bool          // the gateway said hello: the connect worked
	up    time.Duration // wall-clock time from the dial to the drop (0 if the dial failed)
	err   error
	// quiet is the longest the socket went without a frame of Slack's own
	// (an event, a reconnect_url: not a pong), awake time, the drop's
	// stretch included: how often the gateway speaks unasked, which the
	// no-frames watchdog's 2x285 s bound assumes it does (dogfood logs
	// settle it)
	quiet time.Duration
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
			log.Printf("rtm: disconnected (%v) after %s (fast=%v; Slack's own frames at most %s apart), retrying in %s", s.err, s.up.Round(time.Second), r.fast.Load(), s.quiet.Round(time.Second), wait)
			if connReset(s.err) || errors.Is(s.err, errPongTimeout) || errors.Is(s.err, errPingStall) {
				// the path that reset the socket, or went quiet under it,
				// took the pooled API connections with it too
				r.c.ResetConns()
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
	r.helloCh, r.helloOK = make(chan struct{}), false
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.conn = nil
		if r.helloCh != nil && !r.helloOK {
			close(r.helloCh) // no hello: what waits for one gives up
		}
		r.helloCh, r.helloOK = nil, false
		for id, ch := range r.waiters { // the connection went: no reply comes
			close(ch)
			delete(r.waiters, id)
		}
		r.mu.Unlock()
		conn.CloseNow()
		s.up = ReadClock().WallSince(upAt)
	}()

	cctx, stop := context.WithCancel(ctx)
	defer stop()
	// the liveness check (rtmPongTimeout): the last pong, on the wall
	// clock, from the socket's open on as Slack's timer is; pings holds
	// the ids of the pings out
	var pongMu sync.Mutex
	lastPong := upAt
	pings := map[int64]bool{}
	var noPong atomic.Bool
	// the watchdog's clocks: the last frame Slack sent of its own accord
	// (an event, a reconnect_url, hello: not a pong or another answer to
	// one of Clack's frames) and the pong check's last tick; stalled is
	// why it closed the socket
	var watchMu sync.Mutex
	lastFrame, lastTick := upAt, upAt
	var quiet time.Duration // the longest stretch between frames of Slack's own (session.quiet)
	defer func() {
		watchMu.Lock()
		if g := ReadClock().Mono - lastFrame.Mono; g > quiet {
			quiet = g
		}
		s.quiet = quiet
		watchMu.Unlock()
	}()
	var stalled atomic.Pointer[error]
	go func() {
		every := time.Duration(rtmWatchEvery.Load())
		t := time.NewTicker(every)
		defer t.Stop()
		looked := upAt
		for {
			select {
			case <-cctx.Done():
				return
			case <-t.C:
			}
			now := ReadClock()
			gap := now.WallSince(looked)
			looked = now
			if gap > 3*every {
				continue // the machine slept, or the process was held: the pong check sees to the socket
			}
			watchMu.Lock()
			frames, ticks := now.WallSince(lastFrame), now.WallSince(lastTick)
			watchMu.Unlock()
			pongMu.Lock()
			pongs := now.WallSince(lastPong)
			pongMu.Unlock()
			tickMax := time.Duration(rtmTickTimeout.Load())
			if p := 3 * time.Duration(rtmPingEvery.Load()); p > tickMax {
				tickMax = p
			}
			var why error
			reason := noFrames
			switch {
			case frames > time.Duration(rtmFrameTimeout.Load()) && pongs <= time.Duration(rtmPongTimeout.Load()):
				why = errNoFrames
				log.Printf("rtm: nothing from Slack for %s but answers to its pings: closing the socket to connect again (4998 %s); the goroutines follow", frames.Round(time.Second), noFrames)
			case ticks > tickMax:
				why, reason = errPingStall, pingStall
				log.Printf("rtm: the pong check hasn't run for %s: closing the socket to connect again (4998 %s); the goroutines follow", ticks.Round(time.Second), pingStall)
			default:
				continue
			}
			if !r.dumps.Log("rtm: the goroutines", "slack.(*RTM).connect") {
				log.Printf("rtm: (the goroutines were dumped less than %s ago)", r.dumps.Every)
			}
			stalled.Store(&why)
			// as the liveness check closes it: the handshake waits 5 s at
			// most, then the connection drops and Read ends
			_ = conn.Close(websocket.StatusCode(4998), reason)
			return
		}
	}()
	// Slack's timer starts over when a ping's promise resolves: any reply
	// whose reply_to is the ping's id and whose ok isn't false, whatever
	// its type (SlackSocket _onSocketMessage → handleReply)
	pingAnswered := func(data []byte) {
		if !bytes.Contains(data, []byte(`"reply_to"`)) {
			return
		}
		var rep struct {
			OK      *bool `json:"ok"`
			ReplyTo int64 `json:"reply_to"`
		}
		if json.Unmarshal(data, &rep) != nil || rep.OK != nil && !*rep.OK {
			return
		}
		at := ReadClock()
		pongMu.Lock()
		if pings[rep.ReplyTo] {
			delete(pings, rep.ReplyTo)
			lastPong = at
		}
		pongMu.Unlock()
	}
	go func() {
		t := time.NewTicker(time.Duration(rtmPingEvery.Load()))
		defer t.Stop()
		for {
			select {
			case <-cctx.Done():
				return
			case <-t.C:
			}
			at := ReadClock()
			watchMu.Lock()
			lastTick = at
			watchMu.Unlock()
			if h := rtmPingHook.Load(); h != nil {
				(*h)(cctx)
			}
			pongMu.Lock()
			quiet := ReadClock().WallSince(lastPong)
			pongMu.Unlock()
			if quiet > time.Duration(rtmPongTimeout.Load()) {
				log.Printf("rtm: no pong for %s, closing the socket (4998 %s)", quiet.Round(time.Second), pongTimeout)
				noPong.Store(true)
				// the close handshake waits for a peer that may be gone (5 s
				// at most), then drops the connection, which ends Read
				_ = conn.Close(websocket.StatusCode(4998), pongTimeout)
				return
			}
			ping := map[string]any{"type": "ping"}
			if r.Send(ping) == nil {
				if id, ok := ping["id"].(int64); ok {
					pongMu.Lock()
					if len(pings) >= 64 { // pings never answered
						clear(pings)
					}
					pings[id] = true
					pongMu.Unlock()
				}
			}
		}
	}()

	// the events the buffer had no room for, this connection: dropped in
	// all, dropWindow and dropTypes since dropAt (the last line about it)
	var dropped, dropWindow int
	var dropAt ClockReading
	dropTypes := map[string]int{}
	for {
		_, data, err := conn.Read(cctx)
		if err != nil {
			s.err = err
			if noPong.Load() {
				s.err = fmt.Errorf("%w (%w)", errPongTimeout, err)
			} else if why := stalled.Load(); why != nil {
				s.err = fmt.Errorf("%w (%w)", *why, err)
			}
			if dropped > 0 {
				log.Printf("rtm: the socket closed with %d events dropped since the event buffer filled", dropped)
			}
			return s
		}
		pingAnswered(data)
		if r.deliverReply(data) {
			continue
		}
		var ev Event
		if json.Unmarshal(data, &ev) != nil {
			continue
		}
		ev.Raw = data
		if ev.Type != "pong" && ev.Type != "" { // a frame of Slack's own (the watchdog)
			at := ReadClock()
			watchMu.Lock()
			if g := at.Mono - lastFrame.Mono; g > quiet {
				quiet = g
			}
			lastFrame = at
			watchMu.Unlock()
		}
		switch ev.Type {
		case "hello":
			s.hello = true
			log.Printf("rtm: connected in %s", time.Since(t0).Round(time.Millisecond))
			if r.OnState != nil {
				r.OnState(true)
			}
			// after OnState: what waited for hello wakes with the new
			// connection's state in place (core's member epoch moved)
			r.mu.Lock()
			if r.helloCh != nil && !r.helloOK {
				r.helloOK = true
				close(r.helloCh)
			}
			r.mu.Unlock()
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
			// a ping's answer: the socket is alive (Slack's send() resolves
			// the ping on its reply, and the timer starts over)
			at := ReadClock()
			pongMu.Lock()
			lastPong = at
			pongMu.Unlock()
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
			// room again only once the handler has drained half the buffer:
			// one that takes events slower than they come gets one line as
			// it fills and the counts, not two lines per event it takes
			if dropped > 0 && len(r.Events) <= cap(r.Events)/2 {
				log.Printf("rtm: the event buffer has room again (%d wait), after %d events dropped", len(r.Events), dropped)
				dropped, dropWindow = 0, 0
			}
		default:
			// the frames still come, so this is the handler falling
			// behind, not the socket going quiet: one line as it fills,
			// then a count every dropLogEvery, so a flood can't push the
			// watchdog's lines out of the log's queue
			dropped++
			dropWindow++
			dropTypes[ev.Type]++
			switch now := ReadClock(); {
			case dropped == 1:
				log.Printf("rtm: event buffer full (%d events wait for the handler): dropping %s and what follows until it has room; the socket still brings them", len(r.Events), ev.Type)
				dropAt, dropWindow = now, 0
				clear(dropTypes)
			case now.WallSince(dropAt) >= dropLogEvery:
				log.Printf("rtm: event buffer still full: dropped %d more in %s (%s), %d in all", dropWindow, now.WallSince(dropAt).Round(time.Second), dropSummary(dropTypes), dropped)
				dropAt, dropWindow = now, 0
				clear(dropTypes)
			}
			if r.OnDrop != nil {
				r.OnDrop(ev)
			}
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

// activityFrames are the frames the socket manager's send counts as the
// viewer's activity (boot-render's Ls, updateLastSocketActivity: "the time
// we last sent a message over the socket"), the clock maybeTickleMS reads.
var activityFrames = map[string]bool{"tickle": true, "typing": true, "user_typing": true}

// rtmEpoch is activityAt's zero.
var rtmEpoch = time.Now()

// Send writes a JSON frame (assigning an id if absent). A tickle, a
// typing indicator and a user_typing move the activity clock first, as
// the socket manager's send does, whether or not the write goes through.
func (r *RTM) Send(v map[string]any) error {
	if t, _ := v["type"].(string); activityFrames[t] {
		r.activityAt.Store(int64(time.Since(rtmEpoch)) + 1)
	}
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

// GatewayError is the gateway's refusal of a frame Request sent: its
// reply's error (Slack's gatewayErrorCode: 12 the id cap, 15 a rate
// limit).
type GatewayError struct {
	Code int
	Msg  string
}

func (e *GatewayError) Error() string { return fmt.Sprintf("rtm: refused (%d): %s", e.Code, e.Msg) }

// ErrNoReply is a Request whose connection went before its reply came.
var ErrNoReply = errors.New("rtm: the connection closed before the reply")

// ErrHelloWaited is a Request made before its connection said hello: it
// waited for the hello and sent nothing (the state it was made for is the
// last connection's; ask again).
var ErrHelloWaited = errors.New("rtm: the request waited for the connection's hello and wasn't sent")

// Request sends a frame and waits for the gateway's reply to it (Slack's
// sendMessage for socket requests, which resolves on the frame whose
// reply_to is its id): nil when it's ok, a *GatewayError when it says
// ok false, ErrNoReply when the connection went first, ctx's error when
// it ends first.
func (r *RTM) Request(ctx context.Context, v map[string]any) error {
	// connected first: a frame before the gateway's hello isn't taken
	r.mu.Lock()
	conn, hello := r.conn, r.helloCh
	r.mu.Unlock()
	if conn == nil || hello == nil {
		return websocket.CloseError{Code: websocket.StatusGoingAway}
	}
	select {
	case <-hello:
	default:
		// not connected yet: the request was made for the state before
		// this connection, which OnState(true) has started over by the
		// time hello closes; the caller asks again on the new one
		select {
		case <-hello:
		case <-ctx.Done():
			return ctx.Err()
		}
		return ErrHelloWaited
	}
	id := r.next.Add(1)
	v["id"] = id
	ch := make(chan json.RawMessage, 1)
	r.mu.Lock()
	if r.conn != conn || !r.helloOK {
		r.mu.Unlock()
		return ErrNoReply // that connection went before its hello
	}
	if r.waiters == nil {
		r.waiters = map[int64]chan json.RawMessage{}
	}
	r.waiters[id] = ch
	r.mu.Unlock()
	drop := func() {
		r.mu.Lock()
		if r.waiters[id] == ch {
			delete(r.waiters, id)
		}
		r.mu.Unlock()
	}
	if err := r.Send(v); err != nil {
		drop()
		return err
	}
	select {
	case data, ok := <-ch:
		if !ok {
			return ErrNoReply
		}
		var rep struct {
			OK    *bool `json:"ok"`
			Error struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &rep)
		if rep.OK != nil && !*rep.OK {
			return &GatewayError{Code: rep.Error.Code, Msg: rep.Error.Msg}
		}
		return nil
	case <-ctx.Done():
		drop()
		return ctx.Err()
	}
}

// deliverReply hands a reply to the Request waiting for it (true: it was
// one's).
func (r *RTM) deliverReply(data []byte) bool {
	if !bytes.Contains(data, []byte(`"reply_to"`)) {
		return false
	}
	var rep struct {
		ReplyTo int64 `json:"reply_to"`
	}
	if json.Unmarshal(data, &rep) != nil || rep.ReplyTo == 0 {
		return false
	}
	r.mu.Lock()
	ch := r.waiters[rep.ReplyTo]
	delete(r.waiters, rep.ReplyTo)
	r.mu.Unlock()
	if ch == nil {
		return false
	}
	ch <- data
	return true
}

// ClaimActivity is maybeTickleMS's throttle: true when nothing that counts
// as activity went out in the last every (lastSocketActivity), and then
// the clock reads now, as the tickle about to go will make it; false
// otherwise. Two callers at once can't both have it.
func (r *RTM) ClaimActivity(every time.Duration) bool {
	for {
		last := r.activityAt.Load()
		now := int64(time.Since(rtmEpoch)) + 1
		if last != 0 && now-last < int64(every) {
			return false
		}
		if r.activityAt.CompareAndSwap(last, now) {
			return true
		}
	}
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

// SubscribeUsers asks for user_change events for ids, on top of those
// already asked for (Slack's subscribeMembersById: flannel pushes profile
// changes on Grid only for members the client subscribed to), and waits
// for the gateway's answer. UnsubscribeUsers lets them go
// (unsubscribeMembersById). MaxUserSubs is a request's limit (Slack's
// batchMaximum; 101 ids are "Too many updated ids").
const MaxUserSubs = 100

func (r *RTM) SubscribeUsers(ctx context.Context, ids []string) error {
	return r.userChangeRequest(ctx, "user_change_subscribe_request", ids)
}

// UnsubscribeUsers is SubscribeUsers' user_change_unsubscribe_request.
func (r *RTM) UnsubscribeUsers(ctx context.Context, ids []string) error {
	return r.userChangeRequest(ctx, "user_change_unsubscribe_request", ids)
}

func (r *RTM) userChangeRequest(ctx context.Context, subtype string, ids []string) error {
	if len(ids) > MaxUserSubs {
		return fmt.Errorf("rtm: %d ids for %s, at most %d", len(ids), subtype, MaxUserSubs)
	}
	if ids == nil {
		ids = []string{}
	}
	return r.Request(ctx, map[string]any{"type": "subscription_update", "subtype": subtype, "ids": ids})
}

// Connected reports whether the socket is currently up.
func (r *RTM) Connected() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conn != nil
}

// dropLogEvery is how often a full event buffer says how many it dropped.
const dropLogEvery = 10 * time.Second

// dropSummary is the dropped events' types with their counts, the most
// first, five at most.
func dropSummary(types map[string]int) string {
	names := make([]string, 0, len(types))
	for t := range types {
		names = append(names, t)
	}
	sort.Slice(names, func(i, j int) bool {
		if types[names[i]] != types[names[j]] {
			return types[names[i]] > types[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > 5 {
		names = names[:5]
	}
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%s %d", n, types[n])
	}
	return strings.Join(parts, ", ")
}
