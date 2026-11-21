package messages

import (
	"fmt"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// TestThreadPaneAfterSlowConnect: a thread pane at its thread's end goes
// stale at a slow connect with the channel windows (core resets both in
// one go before it tells the lists of the connection; the hub's thread
// model mirrors it, staleThreadEnd): someone's reply the socket brings
// then waits past the stale end, as Slack's addTimestamps puts it in a
// slice of its own, until the pane's read joins it; the viewer's own
// send from the reply box is drawn at once and kept through every
// re-read of a slice that doesn't reach the end yet, as the channel
// list keeps the viewer's sends through its catch-up; the read that
// reaches the end ends that.
func TestThreadPaneAfterSlowConnect(t *testing.T) {
	const ch, root = "C1", "1700000000.000100"
	ts := func(i int) string { return fmt.Sprintf("%d.000100", 1700000000+i) }
	reply := func(i int, user string) core.Message {
		return core.Message{TS: ts(i), ThreadTS: root, Channel: ch, User: user, Text: "a synthetic reply"}
	}
	rootMsg := core.Message{TS: root, ThreadTS: root, Channel: ch, User: "U1", ReplyCount: 2}
	hb := &hub{chans: map[string]*model{}, thread: map[string]*model{}}
	m := hb.threadModel(ch, root)
	slice := func(end int, reachedEnd, prev bool, extra ...core.Message) core.ThreadSlice {
		msgs := []core.Message{rootMsg}
		for i := 1; i <= end; i++ {
			msgs = append(msgs, reply(i, "U2"))
		}
		return core.ThreadSlice{Root: &rootMsg, Msgs: append(msgs, extra...), Start: root, End: ts(end),
			ReachedStart: true, ReachedEnd: reachedEnd, PrevReachedEnd: prev, Held: true}
	}
	m.mu.Lock()
	m.setThreadSlice(slice(2, true, false), nil)
	m.loaded = true
	m.mu.Unlock()
	held := func(i int) bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.find(ts(i)) >= 0
	}

	hb.apply(core.Event{Type: "connection", Connected: true}) // slow: Fast false
	m.mu.Lock()
	reached, prev, gap := m.th.reachedEnd, m.th.prevReachedEnd, m.th.endGap
	m.mu.Unlock()
	if reached || !prev || !gap {
		t.Fatalf("after a slow connect: reachedEnd %v prevReachedEnd %v endGap %v; want the end stale", reached, prev, gap)
	}
	r3 := reply(3, "U3")
	hb.apply(core.Event{Type: "message", Channel: ch, Msg: &r3})
	if held(3) {
		t.Error("someone's reply past the stale end was drawn before the read joined it")
	}
	own := reply(4, "USELF")
	own.Pending = true
	hb.apply(core.Event{Type: "message", Channel: ch, Msg: &own})
	if !held(4) {
		t.Fatal("the viewer's pending reply wasn't drawn")
	}
	// the reload reads the store's slice, the send in a slice of its own
	m.mu.Lock()
	m.setThreadSlice(slice(2, false, true), map[string]bool{})
	m.mu.Unlock()
	if !held(4) {
		t.Error("the reload dropped the viewer's pending reply")
	}
	// a page short of the end: prevReachedEnd gone, the send still drawn
	m.mu.Lock()
	m.setThreadSlice(slice(3, false, false), map[string]bool{})
	m.mu.Unlock()
	if !held(4) || !held(3) {
		t.Errorf("a page short of the end: own reply %v, the page's reply %v; want both", held(4), held(3))
	}
	// the read that reaches the end: the store's slice holds the send
	confirmed := reply(4, "USELF")
	m.mu.Lock()
	m.setThreadSlice(slice(3, true, false, confirmed), nil)
	gap = m.th.endGap
	m.mu.Unlock()
	if !held(4) || gap {
		t.Errorf("at the end again: own reply %v, endGap %v", held(4), gap)
	}
	// a fast reconnect resets nothing
	hb.apply(core.Event{Type: "connection", Connected: true, Fast: true})
	m.mu.Lock()
	reached = m.th.reachedEnd
	m.mu.Unlock()
	if !reached {
		t.Error("a fast reconnect made the thread's end stale")
	}
}
