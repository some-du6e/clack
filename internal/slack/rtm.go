package slack

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
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

	// OnHello runs after every (re)connect, e.g. to resend subscriptions.
	OnHello func()
}

func (c *Client) NewRTM() *RTM {
	return &RTM{c: c, Events: make(chan Event, 1024)}
}

// Run blocks until ctx is done, reconnecting with exponential backoff.
func (r *RTM) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := r.once(ctx)
		if ctx.Err() != nil {
			return
		}
		if r.OnState != nil {
			r.OnState(false)
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
			r.fails = 0
		} else {
			r.fails++
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
	if u == "" {
		u = r.c.WebSocketURL(ctx, r.fails%3 == 2) // every third failure, try the backup gateway
	}
	t0 := time.Now()
	h := http.Header{}
	h.Set("Cookie", r.c.cookieHeader())
	h.Set("Origin", "https://app.slack.com")
	h.Set("User-Agent", UserAgent)
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, _, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPHeader: h, HTTPClient: r.c.HTTP, CompressionMode: websocket.CompressionContextTakeover})
	cancel()
	if err != nil {
		return err
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
			log.Printf("rtm: gateway error frame: %.200s", data)
		case "reconnect_url":
			var x struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(data, &x)
			r.reconnectURL = x.URL
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
func (r *RTM) SubscribeUsers(ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	return r.Send(map[string]any{"type": "subscription_update", "subtype": "user_change_subscribe_request", "ids": ids})
}

// Connected reports whether the socket is currently up.
func (r *RTM) Connected() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conn != nil
}
