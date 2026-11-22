package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// pongGateway is a realtime gateway whose sockets answer Slack's JSON
// pings with pongs while answer is set, as the real gateway does, and go
// silent (no pong, no frame, no close) while it isn't: a socket after a
// sleep, a lost NAT mapping, a half-open TCP connection. Each socket's
// close status from the client arrives on closed.
type pongGateway struct {
	srv    *httptest.Server
	answer atomic.Bool
	// reply, when set, is the answer's shape instead of a pong:
	// `{"ok":true,"reply_to":%d}` (any frame with the ping's reply_to)
	reply  atomic.Value
	dials  atomic.Int32
	closed chan websocket.CloseError
	conns  chan *websocket.Conn
}

func newPongGateway(t *testing.T) (*pongGateway, *Client) {
	g := &pongGateway{closed: make(chan websocket.CloseError, 16), conns: make(chan *websocket.Conn, 16)}
	g.answer.Store(true)
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/client.getWebSocketURL":
			ws := "ws" + strings.TrimPrefix(g.srv.URL, "http") + "/ws/slow"
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "primary_websocket_url": ws, "fallback_websocket_url": ws})
		case strings.HasPrefix(r.URL.Path, "/ws/"):
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.CloseNow()
			g.dials.Add(1)
			ctx := r.Context()
			_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello"}`))
			g.conns <- c
			for {
				_, data, err := c.Read(ctx)
				if err != nil {
					var ce websocket.CloseError
					if errors.As(err, &ce) {
						g.closed <- ce
					} else {
						g.closed <- websocket.CloseError{Code: -1, Reason: err.Error()}
					}
					return
				}
				var f struct {
					Type string `json:"type"`
					ID   int64  `json:"id"`
				}
				if json.Unmarshal(data, &f) == nil && f.Type == "ping" && g.answer.Load() {
					b, _ := json.Marshal(map[string]any{"type": "pong", "reply_to": f.ID})
					if shape, _ := g.reply.Load().(string); shape != "" {
						b = []byte(fmt.Sprintf(shape, f.ID))
					}
					_ = c.Write(ctx, websocket.MessageText, b)
				}
			}
		default:
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(g.srv.Close)
	return g, New(Creds{Token: "xoxc-test", Team: "T1", TeamURL: g.srv.URL + "/"})
}

func pongTimers(t *testing.T, every, timeout time.Duration) {
	t.Cleanup(SetPingTimers(every, timeout))
}

func (g *pongGateway) nextConn(t *testing.T) {
	t.Helper()
	select {
	case <-g.conns:
	case <-time.After(5 * time.Second):
		t.Fatal("the client didn't connect")
	}
}

func (g *pongGateway) nextClose(t *testing.T, within time.Duration) websocket.CloseError {
	t.Helper()
	select {
	case ce := <-g.closed:
		return ce
	case <-time.After(within):
		t.Fatalf("the client kept the socket for %s", within)
		return websocket.CloseError{}
	}
}

// A socket that goes silent (no pong, nothing at all, and no close frame)
// is closed with Slack's 4998 "Too long since we received a pong" once
// rtmPongTimeout passes without a pong, and Run connects again: Slack's
// SlackSocket (client-boot Kot3 _onSocketOpen). Before, Clack sent its
// pings and never looked for an answer, so a socket that died quietly
// blocked Read for hours and every event in between was lost. A socket
// whose pongs come keeps going.
func TestRTMClosesSilentSocket(t *testing.T) {
	fastBackoff(t)
	pongTimers(t, 10*time.Millisecond, 300*time.Millisecond)
	logs := captureLog(t)
	g, c := newPongGateway(t)
	r := c.NewRTM()
	runRTM(t, r)
	g.nextConn(t)

	// answered pings: the socket lives well past the timeout
	select {
	case ce := <-g.closed:
		t.Fatalf("a socket whose pings were answered was closed (%d %q)", ce.Code, ce.Reason)
	case <-time.After(1200 * time.Millisecond):
	}
	if n := g.dials.Load(); n != 1 {
		t.Fatalf("%d dials while the pongs came, want 1", n)
	}

	// silent: closed with 4998 within the timeout and a tick or two
	g.answer.Store(false)
	t0 := time.Now()
	ce := g.nextClose(t, 5*time.Second)
	if ce.Code != 4998 || ce.Reason != pongTimeout {
		t.Errorf("the silent socket was closed with %d %q, want 4998 %q", ce.Code, ce.Reason, pongTimeout)
	}
	if d := time.Since(t0); d < 250*time.Millisecond {
		t.Errorf("closed %s after the pongs stopped, before the 300ms timeout", d)
	}
	g.answer.Store(true)
	g.nextConn(t) // and it connects again
	if !strings.Contains(logs.String(), "no pong for") {
		t.Errorf("no line for the closed socket:\n%s", logs.String())
	}
}

// The timeout is wall-clock time, as Slack's Date.now() is: a socket that
// slept through it is closed at the next tick after the wake, though the
// monotonic clock (which stops while a Mac or Linux box sleeps) moved
// only milliseconds.
func TestRTMPongTimeoutCountsSleep(t *testing.T) {
	fastBackoff(t)
	clk := newFakeClock(t)
	pongTimers(t, 10*time.Millisecond, time.Hour)
	g, c := newPongGateway(t)
	r := c.NewRTM()
	runRTM(t, r)
	g.nextConn(t)
	time.Sleep(100 * time.Millisecond) // pongs come
	select {
	case ce := <-g.closed:
		t.Fatalf("closed before the sleep (%d %q)", ce.Code, ce.Reason)
	default:
	}
	g.answer.Store(false) // the machine is asleep: nothing answers
	time.Sleep(30 * time.Millisecond)
	clk.sleep(2 * time.Hour)
	ce := g.nextClose(t, 5*time.Second)
	if ce.Code != 4998 {
		t.Errorf("after the sleep the socket was closed with %d %q, want 4998", ce.Code, ce.Reason)
	}
	g.answer.Store(true)
	g.nextConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for !r.Connected() && ctx.Err() == nil {
		time.Sleep(5 * time.Millisecond)
	}
	if !r.Connected() {
		t.Error("not connected again after the wake")
	}
}

// Slack's timer starts over on any reply to the ping (its reply_to), not
// only a frame of type pong: SlackSocket's send() resolves the ping on its
// reply whatever the type, unless the reply says ok:false. A socket whose
// pings get such replies lives on; one whose replies refuse them is closed.
func TestRTMPongIsAnyReplyToThePing(t *testing.T) {
	fastBackoff(t)
	pongTimers(t, 10*time.Millisecond, 300*time.Millisecond)
	g, c := newPongGateway(t)
	g.reply.Store(`{"ok":true,"reply_to":%d}`)
	r := c.NewRTM()
	runRTM(t, r)
	g.nextConn(t)
	select {
	case ce := <-g.closed:
		t.Fatalf("a socket whose pings got replies was closed (%d %q)", ce.Code, ce.Reason)
	case <-time.After(1200 * time.Millisecond):
	}
	g.reply.Store(`{"ok":false,"reply_to":%d,"error":{"msg":"no"}}`)
	if ce := g.nextClose(t, 5*time.Second); ce.Code != 4998 {
		t.Errorf("a socket whose pings were refused was closed with %d %q, want 4998", ce.Code, ce.Reason)
	}
}
