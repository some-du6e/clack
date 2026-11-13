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

	mu           sync.Mutex
	conn         *websocket.Conn
	next         atomic.Int64
	reconnectURL string
	fails        int
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
	return &RTM{c: c, Events: make(chan Event, 1024)}
}

// MaxRTMFails is how many short-lived connects in a row Run tries before it
// gives up (a connection that lasted over a minute resets the count).
const MaxRTMFails = 8

// Run blocks until ctx is done, reconnecting with exponential backoff, or
// until MaxRTMFails connects in a row failed or the session ended.
func (r *RTM) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil && !r.c.SignedOut() {
		start := time.Now()
		err := r.once(ctx)
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
		if time.Since(start) > time.Minute {
			backoff = time.Second
			r.fails = 0
		} else {
			r.fails++
		}
		if r.fails >= MaxRTMFails {
			// Don't keep hammering the gateway: core falls back to polling.
			log.Printf("rtm: disconnected (%v); %d failed connects in a row, giving up until live updates are turned on again", err, r.fails)
			r.fails = 0
			return
		}
		log.Printf("rtm: disconnected (%v), retrying in %s", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (r *RTM) once(ctx context.Context) error {
	u := r.reconnectURL
	r.reconnectURL = ""
	r.fast.Store(u != "")
	if u == "" {
		u = r.c.WebSocketURL(ctx, r.fails%3 == 2) // every third failure, try the backup gateway
	}
	// The URL carries the token and the dial sends the cookie: it must be
	// on slack.com, whatever client.getWebSocketURL or a reconnect_url said.
	if pu, err := url.Parse(u); err != nil || !r.c.credsOK(pu) {
		return ErrUntrustedHost
	}
	if r.c.SignedOut() {
		return ErrSignedOut
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
		return scrubErr(err) // the dial error quotes the URL, token included
	}
	conn.SetReadLimit(64 << 20)
	r.mu.Lock()
	r.conn = conn
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.conn = nil
		r.mu.Unlock()
		conn.CloseNow()
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
			return err
		}
		var ev Event
		if json.Unmarshal(data, &ev) != nil {
			continue
		}
		ev.Raw = data
		switch ev.Type {
		case "hello":
			log.Printf("rtm: connected in %s", time.Since(t0).Round(time.Millisecond))
			if r.OnState != nil {
				r.OnState(true)
			}
			if r.OnHello != nil {
				go r.OnHello()
			}
		case "error":
			var fe struct {
				Error struct {
					Code any    `json:"code"`
					Msg  string `json:"msg"`
				} `json:"error"`
			}
			_ = json.Unmarshal(data, &fe)
			log.Printf("rtm: gateway error frame: %s", fe.Error.Msg)
			for code := range authCodes {
				if strings.Contains(fe.Error.Msg, code) {
					r.c.markAuth(code)
					return errors.New("rtm: " + code)
				}
			}
		case "reconnect_url":
			var x struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(data, &x)
			if slackURL(x.URL, "wss") { // it carries the token: Slack hosts only
				r.reconnectURL = x.URL
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
			return nil
		}
		select {
		case r.Events <- ev:
		default:
			log.Printf("rtm: event buffer full, dropping %s", ev.Type)
		}
	}
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
