package slack

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The socket's watchdog (rtm.go), beside the liveness check: a socket
// whose pong check stopped running (a ping stuck in its write) is closed
// with 4998 and Run connects again, every goroutine's stack in the log; a
// healthy idle socket, Slack's reconnect_url now and then and its pongs,
// is left be; and a full event buffer says once that the events still
// come, and again when it has room, never that the socket went quiet.

// takeConn is the gateway's next accepted socket, to write frames to.
func (g *pongGateway) takeConn(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case c := <-g.conns:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("the client didn't connect")
		return nil
	}
}

func rtmWait(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The pong check's goroutine stops at a tick (a ping stuck in its
// write): no check runs, so nothing closes a socket that went quiet. The
// watchdog closes it with 4998 once the check hasn't run for its timeout,
// with the goroutines in the log, and Run connects again. Clack waited
// on the stuck check for good.
func TestRTMReconnectsWhenThePongCheckStalls(t *testing.T) {
	fastBackoff(t)
	pongTimers(t, 20*time.Millisecond, time.Hour) // the check itself never closes it
	watchTimers(t, time.Hour, 300*time.Millisecond, 50*time.Millisecond)
	logs := captureLog(t)
	g, c := newPongGateway(t)
	r := c.NewRTM()
	runRTM(t, r)
	g.nextConn(t)
	release := stallPongCheck(t)
	ce := g.nextClose(t, 5*time.Second)
	release()
	if ce.Code != 4998 || ce.Reason != "Too long since the pong check ran" {
		t.Errorf("the socket was closed with %d %q, want 4998 %q", ce.Code, ce.Reason, "Too long since the pong check ran")
	}
	g.nextConn(t) // and it connects again
	out := logs.String()
	if !strings.Contains(out, "rtm: the pong check hasn't run for") {
		t.Errorf("no line for the stalled check:\n%s", out)
	}
	if !strings.Contains(out, "all goroutines:") || !strings.Contains(out, "slack.(*RTM).connect") {
		t.Error("no goroutine dump with the socket's goroutines in the log")
	}
}

// A healthy idle socket: pongs, and Slack's reconnect_url every so often,
// no event. The watchdog leaves it be, however long it stays idle.
func TestRTMKeepsAnIdleSocket(t *testing.T) {
	fastBackoff(t)
	pongTimers(t, 20*time.Millisecond, 3*time.Second)
	watchTimers(t, 600*time.Millisecond, 600*time.Millisecond, 50*time.Millisecond)
	logs := captureLog(t)
	g, c := newPongGateway(t)
	r := c.NewRTM()
	runRTM(t, r)
	conn := g.takeConn(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		tk := time.NewTicker(100 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
			}
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"reconnect_url","url":"wss://wss-primary.slack.com/link/?ticket=synthetic"}`))
		}
	}()
	select {
	case ce := <-g.closed:
		t.Fatalf("the idle socket was closed (%d %q)", ce.Code, ce.Reason)
	case <-time.After(3 * time.Second):
	}
	if n := g.dials.Load(); n != 1 {
		t.Errorf("%d dials for an idle socket, want 1", n)
	}
	for _, bad := range []string{"nothing from Slack", "pong check hasn't run", "all goroutines:"} {
		if strings.Contains(logs.String(), bad) {
			t.Errorf("the idle socket logged %q:\n%s", bad, logs.String())
		}
	}
}

// The handler falls behind and the event buffer fills while the socket
// keeps bringing events: one line says so, that the events still come
// (not the socket going quiet), and one more when the buffer has room
// again, with how many it dropped. Clack logged a line per dropped event,
// a flood that pushed every other line out of the log's queue.
func TestRTMBufferFullIsNotSilence(t *testing.T) {
	fastBackoff(t)
	pongTimers(t, 20*time.Millisecond, 3*time.Second)
	logs := captureLog(t)
	g, c := newPongGateway(t)
	r := c.NewRTM()
	r.Events = make(chan Event, 2) // nobody reads it yet: the handler fell behind
	var dropped atomic.Int32
	r.OnDrop = func(Event) { dropped.Add(1) }
	runRTM(t, r)
	conn := g.takeConn(t)
	// hello goes into Events too: the handler takes it, and the buffer's
	// two places are free
	select {
	case ev := <-r.Events:
		if ev.Type != "hello" {
			t.Fatalf("the first event is %q, want hello", ev.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no hello")
	}
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		_ = conn.Write(ctx, websocket.MessageText, []byte(fmt.Sprintf(`{"type":"user_typing","channel":"C0SYNTH001","user":"U0SYNTH%03d"}`, i)))
	}
	rtmWait(t, 5*time.Second, "28 events dropped", func() bool { return dropped.Load() == 28 })
	out := logs.String()
	if n := strings.Count(out, "event buffer full"); n != 1 {
		t.Errorf("%d lines for one overflow, want one:\n%s", n, out)
	}
	if !strings.Contains(out, "the socket still brings them") {
		t.Errorf("the overflow's line doesn't say the events still come:\n%s", out)
	}
	<-r.Events // the handler catches up
	<-r.Events
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"user_typing","channel":"C0SYNTH001","user":"U0SYNTH999"}`))
	rtmWait(t, 5*time.Second, "the room-again line", func() bool {
		return strings.Contains(logs.String(), "the event buffer has room again, after 28 events dropped")
	})
	if strings.Contains(logs.String(), "nothing from Slack") {
		t.Error("an overflow was taken for a quiet socket")
	}
}
