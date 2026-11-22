package messages

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestThreadReplyBoxStaysAfterSlowConnect (threads-ship finding 7): a pane
// at its thread's end keeps its reply box through a slow connect until
// its read past the old end lands, also once a reply the socket brought
// makes the store's slice for the pane no longer the thread's last (its
// prevReachedEnd goes: Slack's reducer gives that reply a slice of its own
// past the stale end). The pane had reached the end (endGap), and Clack's
// box sits under the list, where the viewer may be typing: Slack's input
// is a row of the list, scrolled away with it. The code before hid the
// box at the next re-read of the store (the 30 s reload, a loadPre
// landing) and drew the "loading-end" row in its place, and what was
// typed then was dropped. Synthetic data only.
func TestThreadReplyBoxStaysAfterSlowConnect(t *testing.T) {
	app, _, ts := shotsApp(t)
	host := coreHost{app: app, th: theme.New(false)}
	root := ts["c1"]
	tv := NewThreadView(host, tShots, root)
	s, m := tv.s, tv.s.m
	// the pane's own first load lands before the test sets the model
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		m.mu.Lock()
		done := m.loaded && !m.loading
		m.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the pane's first load never landed")
		}
	}
	sec, _ := strconv.ParseInt(strings.SplitN(root, ".", 2)[0], 10, 64)
	at := func(i int) string { return strconv.FormatInt(sec+int64(i), 10) + ".000100" }
	rootMsg := core.Message{TS: root, ThreadTS: root, Channel: tShots, User: "UCARA0001", Text: "x", ReplyCount: 3}
	reply := func(i int) core.Message {
		return core.Message{TS: at(i), ThreadTS: root, Channel: tShots, User: "UCARA0001", Text: "a synthetic reply"}
	}
	slice := func(reachedEnd bool) core.ThreadSlice {
		return core.ThreadSlice{Root: &rootMsg, Msgs: []core.Message{rootMsg, reply(1), reply(2)}, Start: root, End: at(2),
			ReachedStart: true, ReachedEnd: reachedEnd, Held: true}
	}
	view := func() {
		s.snap.rev = 0
		m.snapshot(&s.snap)
		s.build(host, time.Now())
	}
	loadingEnd := func() bool {
		for _, it := range s.items {
			if it.key == "loading-end" {
				return true
			}
		}
		return false
	}
	m.mu.Lock()
	m.setThreadSlice(slice(true), nil)
	m.loaded = true
	m.mu.Unlock()
	view()
	if tv.HidesComposer() || loadingEnd() {
		t.Fatal("no reply box at the thread's end")
	}

	s.hb.apply(core.Event{Type: "connection", Connected: true}) // slow: the end goes stale
	view()
	if tv.HidesComposer() || loadingEnd() {
		t.Fatal("the reply box went at the slow connect")
	}
	// a reply the socket brought sits past the stale end in a slice of its
	// own; the store's next slice for the pane has no prevReachedEnd
	m.mu.Lock()
	m.setThreadSlice(slice(false), map[string]bool{})
	m.mu.Unlock()
	view()
	if tv.HidesComposer() {
		t.Error("the reply box went at the store's re-read after a reply past the stale end: what's typed there is lost")
	}
	if loadingEnd() {
		t.Error("the loading-end row took the reply box's place before the pane's read past the old end landed")
	}
	// that read reaches the end: the box stays, and the gap is over
	m.mu.Lock()
	m.setThreadSlice(slice(true), nil)
	gap := m.th.endGap
	m.mu.Unlock()
	view()
	if tv.HidesComposer() || gap {
		t.Errorf("at the end again: the box hidden %v, endGap %v", tv.HidesComposer(), gap)
	}
	// a slice short of the end the pane never reached has no box, as before
	m.mu.Lock()
	m.th = threadState{gen: m.th.gen + 1}
	m.setThreadSlice(slice(false), nil)
	m.mu.Unlock()
	view()
	if !tv.HidesComposer() || !loadingEnd() {
		t.Error("a slice short of the thread's end, never at it, shows the reply box")
	}
}
