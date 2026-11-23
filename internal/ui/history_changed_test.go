package ui

// History rewritten on Slack's side (a bulk delete, retention) and told
// with channel_history_changed (core/history_changed.go): the lists that
// drew what changed draw it no more. The open channel, a jump's window
// (one that crosses the event's latest, and one wholly before it), the
// thread pane and a conversation shown earlier (its model cached) each
// held the deleted messages as ghosts. The fake deletes quietly and
// sends the event. Synthetic data only.

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// hcEnv is the app with ch open at its newest messages and the socket up.
type hcEnv struct {
	*perfEnv
	fs  *fakeslack.Server
	ch  string
	tss []string // ch's history on the fake, oldest first
}

func newUIHCEnv(t *testing.T) *hcEnv {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := &hcEnv{perfEnv: startEnv(t, fs, t.TempDir()), fs: fs}
	e.waitReady()
	ids := fs.ChannelIDs()
	e.ch = ids[2]
	e.tss = fs.HistoryTS(e.ch)
	if len(e.tss) < 60 {
		t.Fatalf("%s has %d messages; the test needs 60", e.ch, len(e.tss))
	}
	e.openChannel(e.ch)
	e.until("the channel's history", 20*time.Second, func() bool { return e.loadedMessages(e.ch) > 0 })
	e.toLatest()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() && fs.Sockets() > 0 })
	return e
}

// fence pushes a message into ch and waits for the list to hold it:
// every event pushed before it reached the lists.
func (e *hcEnv) fence(v *messages.ChannelView) {
	e.tb.Helper()
	ts, ev := e.fs.PostQuiet(e.ch, e.fs.UserIDs()[3], "fence")
	e.fs.Push(ev)
	e.until("the fence message", 10*time.Second, func() bool { return hcHolds(v.LoadedTS(), ts) })
}

func hcHolds(tss []string, ts string) bool {
	for _, x := range tss {
		if x == ts {
			return true
		}
	}
	return false
}

// hcGhosts is the ts among gone that tss holds.
func hcGhosts(tss, gone []string) []string {
	g := map[string]bool{}
	for _, ts := range gone {
		g[ts] = true
	}
	var out []string
	for _, ts := range tss {
		if g[ts] {
			out = append(out, ts)
		}
	}
	return out
}

// noGhosts waits for list() to hold none of gone.
func (e *hcEnv) noGhosts(what string, list func() []string, gone []string) {
	e.tb.Helper()
	dl := time.Now().Add(10 * time.Second)
	for {
		g := hcGhosts(list(), gone)
		if len(g) == 0 {
			return
		}
		if time.Now().After(dl) {
			e.tb.Fatalf("%s: the list still holds %d deleted messages (%v)", what, len(g), g)
		}
		e.frame()
		time.Sleep(2 * time.Millisecond)
	}
}

func TestHistoryChangedLists(t *testing.T) {
	// the open channel: what was cached before the event's latest goes
	t.Run("open channel", func(t *testing.T) {
		e := newUIHCEnv(t)
		v := e.ownSendList(e.ch)
		loaded := v.LoadedTS()
		if len(loaded) < 30 {
			t.Fatalf("control: the list holds %d messages", len(loaded))
		}
		n := len(loaded)
		gone, latest := append([]string(nil), loaded[n-25:n-15]...), loaded[n-15]
		e.fs.DeleteQuiet(e.ch, gone...)
		e.fs.Push(fakeslack.HistoryChangedEvent("channel_history_changed", e.ch, latest, false))
		e.fence(v)
		e.noGhosts("the open channel", v.LoadedTS, gone)
		if !hcHolds(v.LoadedTS(), loaded[n-1]) {
			t.Errorf("the newest message went with the history before the change")
		}
	})
	// a jump's window: one crossing the event's latest keeps what is after
	// it; one wholly before it loads again around its newest message
	for _, tc := range []struct {
		name   string
		latest func(e *hcEnv) string
	}{
		{"window crossing the change", func(e *hcEnv) string { return e.tss[9] }},
		{"window before the change", func(e *hcEnv) string { return e.tss[len(e.tss)-1] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newUIHCEnv(t)
			v := e.ownSendList(e.ch)
			old := e.tss[5]
			v.Highlight(old)
			e.until("the window around an old message", 20*time.Second, func() bool { return v.InWindow() && hcHolds(v.LoadedTS(), old) })
			gone := append([]string(nil), e.tss[3:9]...)
			if g := hcGhosts(v.LoadedTS(), gone); len(g) != len(gone) {
				t.Fatalf("control: the window holds %d of the %d messages to delete", len(g), len(gone))
			}
			e.fs.DeleteQuiet(e.ch, gone...)
			e.fs.Push(fakeslack.HistoryChangedEvent("channel_history_changed", e.ch, tc.latest(e), false))
			e.settle(300 * time.Millisecond)
			e.noGhosts("the window", v.LoadedTS, gone)
		})
	}
	// the thread pane: its replies before the event's latest go
	t.Run("thread pane", func(t *testing.T) {
		e := newUIHCEnv(t)
		root := e.tss[len(e.tss)-4]
		user := e.fs.UserIDs()[2]
		var rs []string
		for i := 0; i < 6; i++ {
			rs = append(rs, e.fs.PostReplyQuiet(e.ch, root, user, fmt.Sprintf("synthetic reply %d", i)))
		}
		e.a.OpenThread(e.ch, root)
		rows := func() []string {
			var out []string
			for _, r := range e.paneRows("", e.ch, root) {
				out = append(out, r.TS)
			}
			return out
		}
		e.until("the thread's replies", 20*time.Second, func() bool { return len(hcGhosts(rows(), rs)) == len(rs) })
		for _, ts := range rs[:2] {
			if !e.fs.DeleteReplyQuiet(e.ch, root, ts) {
				t.Fatalf("control: the fake has no reply %s", ts)
			}
		}
		e.fs.Push(fakeslack.HistoryChangedEvent("channel_history_changed", e.ch, rs[2], false))
		e.fence(e.ownSendList(e.ch))
		e.noGhosts("the thread pane", rows, rs[:2])
		if !hcHolds(rows(), rs[5]) {
			t.Errorf("the thread's newest reply went with the replies before the change: %v", rows())
		}
	})
	// a conversation shown earlier: shown again, it draws Slack's history
	t.Run("conversation shown earlier", func(t *testing.T) {
		e := newUIHCEnv(t)
		v := e.ownSendList(e.ch)
		loaded := v.LoadedTS()
		n := len(loaded)
		gone, latest := append([]string(nil), loaded[n-20:n-10]...), loaded[n-10]
		other := e.fs.ChannelIDs()[3]
		e.openChannel(other)
		e.until("the other channel", 20*time.Second, func() bool { return e.loadedMessages(other) > 0 })
		e.settle(200 * time.Millisecond)
		e.fs.DeleteQuiet(e.ch, gone...)
		e.fs.Push(fakeslack.HistoryChangedEvent("channel_history_changed", e.ch, latest, false))
		e.settle(500 * time.Millisecond)
		e.openChannel(e.ch)
		e.toLatest()
		v = e.ownSendList(e.ch)
		e.noGhosts("the conversation shown again", v.LoadedTS, gone)
	})
}
