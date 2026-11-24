package core

// History rewritten on Slack's side (a bulk delete, retention): Slack
// tells the clients with channel_history_changed and with client.counts'
// history_invalid and updated, and the client lets go of what it cached
// (history_changed.go). Without them the deleted messages stayed in the
// cached history as ghosts, and the edited ones in their old text. The
// fake rewrites its history quietly and sends the event, or the counts.
// Synthetic data only.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// hcEnv is rtApp with ch's newest page cached, read forward after the
// connect (newHCEnv).
type hcEnv struct {
	t   *testing.T
	a   *App
	fs  *fakeslack.Server
	log *eventLog
	ch  string
	tss []string // ch's history on the fake, oldest first
}

// newHCEnv holds the socket's hello until boot's prefetch has read ch, one
// of the fake's unread channels, so the slow connect makes ch's window end
// stale and the first History reads it forward (reached_end.go catchUp), as
// when the prefetch beats the connect. It waits for that read: its
// conversations.history isn't the tests' to count. Left racing it, the
// read sometimes went out after a test had counted the calls before its
// own (TestCountsFirstAnswerIsBaseline: "the baseline counts read the
// history again (1 conversations.history)", full-test 45313234515).
func newHCEnv(t *testing.T) *hcEnv {
	t.Helper()
	var ch string
	a, fs, l := rtAppHeld(t, func(a *App, fs *fakeslack.Server) bool {
		ch = fs.ChannelIDs()[1]
		return a.S.Timeline(ch) != nil
	})
	e := &hcEnv{t: t, a: a, fs: fs, log: l, ch: ch}
	e.tss = fs.HistoryTS(e.ch)
	if len(e.tss) < 40 {
		t.Fatalf("%s has %d messages; the test needs 40", e.ch, len(e.tss))
	}
	if _, err := a.History(context.Background(), e.ch, ""); err != nil {
		t.Fatal(err)
	}
	e.until("the connect's catch-up of the prefetched window", func() bool {
		_, _, stale := a.S.EndOf(e.ch)
		return !stale
	})
	return e
}

// page is ch's newest page as History gives it.
func (e *hcEnv) page() []string {
	e.t.Helper()
	p, err := e.a.History(context.Background(), e.ch, "")
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for i := range p.Messages {
		out = append(out, p.Messages[i].TS)
	}
	return out
}

// ghosts is the ts among gone that tss holds.
func ghosts(tss, gone []string) []string {
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

// until polls cond for up to 10 s.
func (e *hcEnv) until(what string, cond func() bool) {
	e.t.Helper()
	for dl := time.Now().Add(10 * time.Second); !cond(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(dl) {
			e.t.Fatalf("timed out waiting for %s; events: %s", what, evTypes(e.log.all()))
		}
	}
}

// fence pushes a message into ch and waits for core's event of it:
// every event pushed before it was handled. (Not for the message in the
// store: with no history of ch cached, there's none to hold it.)
func (e *hcEnv) fence() {
	e.t.Helper()
	ts, ev := e.fs.PostQuiet(e.ch, e.fs.UserIDs()[3], "fence")
	e.fs.Push(ev)
	e.log.wait(e.t, "the fence message", func(evs []Event) bool {
		for _, x := range evs {
			if x.Type == "message" && x.Channel == e.ch && (x.TS == ts || x.Msg != nil && x.Msg.TS == ts) {
				return true
			}
		}
		return false
	})
}

// A channel_history_changed for a cached conversation: what was cached
// before its latest goes, so the deleted messages are no longer drawn,
// and the older pages come from Slack again, without them.
func TestHistoryChangedDropsCachedMessages(t *testing.T) {
	e := newHCEnv(t)
	n := len(e.tss)
	gone := append([]string(nil), e.tss[n-30:n-20]...)
	if g := ghosts(e.page(), gone); len(g) != len(gone) {
		t.Fatalf("control: the cached page holds %d of the 10 messages to delete", len(g))
	}
	if d := e.fs.DeleteQuiet(e.ch, gone...); d != len(gone) {
		t.Fatalf("control: the fake deleted %d of %d", d, len(gone))
	}
	e.fs.Push(fakeslack.HistoryChangedEvent("channel_history_changed", e.ch, e.tss[n-20], false))
	e.fence()
	if g := ghosts(e.page(), gone); len(g) > 0 {
		t.Errorf("after channel_history_changed the cached page still holds %d deleted messages (%v)", len(g), g)
	}
	// the older page, read again: from Slack, without them
	p := e.page()
	if len(p) == 0 {
		t.Fatal("the page is empty")
	}
	older, err := e.a.History(context.Background(), e.ch, p[0])
	if err != nil {
		t.Fatal(err)
	}
	var tss []string
	for i := range older.Messages {
		tss = append(tss, older.Messages[i].TS)
	}
	if g := ghosts(tss, gone); len(g) > 0 {
		t.Errorf("the older page holds %d deleted messages (%v)", len(g), g)
	}
}

// A thread of the conversation: its replies before the event's latest go
// with the rest, so a reply deleted on Slack's side leaves the thread.
func TestHistoryChangedDropsCachedReplies(t *testing.T) {
	e := newHCEnv(t)
	root := e.tss[len(e.tss)-5]
	user := e.fs.UserIDs()[2]
	var rs []string
	for i := 0; i < 6; i++ {
		rs = append(rs, e.fs.PostReplyQuiet(e.ch, root, user, fmt.Sprintf("synthetic reply %d", i)))
	}
	ctx := context.Background()
	sl, err := e.a.ThreadAt(ctx, e.ch, root, root)
	if err != nil {
		t.Fatal(err)
	}
	replies := func(sl ThreadSlice) []string {
		var out []string
		for i := range sl.Msgs {
			out = append(out, sl.Msgs[i].TS)
		}
		return out
	}
	if g := ghosts(replies(sl), rs[:2]); len(g) != 2 {
		t.Fatalf("control: the thread's slice holds %d of the 2 replies to delete (%v)", len(g), replies(sl))
	}
	for _, ts := range rs[:2] {
		if !e.fs.DeleteReplyQuiet(e.ch, root, ts) {
			t.Fatalf("control: the fake has no reply %s", ts)
		}
	}
	e.fs.Push(fakeslack.HistoryChangedEvent("channel_history_changed", e.ch, rs[2], false))
	e.fence()
	sl, err = e.a.ThreadAt(ctx, e.ch, root, root)
	if err != nil {
		t.Fatal(err)
	}
	if g := ghosts(replies(sl), rs[:2]); len(g) > 0 {
		t.Errorf("after channel_history_changed the thread still holds %d deleted replies (%v)", len(g), g)
	}
}

// The message subtype of the same name is no message: Slack's
// messageReceived ignores it.
func TestHistoryChangedSubtypeIsNoMessage(t *testing.T) {
	e := newHCEnv(t)
	ev := fakeslack.HistoryChangedEvent("channel_history_changed", e.ch, e.tss[0], true)
	e.fs.Push(ev)
	e.fence()
	if m, ok := e.a.S.Find(e.ch, ev["ts"].(string)); ok {
		t.Errorf("the channel_history_changed message subtype is a message in the history: %+v", m)
	}
}

// client.counts' history_invalid newer than the applied one purges the
// conversation's cached history: the newest page comes from Slack again,
// without what was deleted.
func TestCountsHistoryInvalidPurgesHistory(t *testing.T) {
	e := newHCEnv(t)
	n := len(e.tss)
	old := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	e.baseline(old)
	gone := append([]string(nil), e.tss[n-12:n-8]...)
	e.fs.DeleteQuiet(e.ch, gone...)
	now := time.Now().Unix()
	e.fs.SetMutationTimestamps(e.ch, fmt.Sprintf("%d.000100", now), fmt.Sprintf("%d.000100", now))
	e.a.refreshCounts(context.Background())
	if g := ghosts(e.page(), gone); len(g) > 0 {
		t.Errorf("after counts with a newer history_invalid the page still holds %d deleted messages (%v)", len(g), g)
	}
}

// client.counts' updated newer than the applied one marks the cached
// history stale: the next read draws it and reads it again, and the
// lists hear when the new copy is in ("history_caught_up").
func TestCountsUpdatedRereadsHistory(t *testing.T) {
	e := newHCEnv(t)
	ctx := context.Background()
	hi := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	// the applied timestamps, before anything changed
	e.fs.SetMutationTimestamps(e.ch, hi, hi)
	e.a.refreshCounts(ctx)
	e.page()
	target := e.tss[len(e.tss)-3]
	if !e.fs.EditQuiet(e.ch, target, "synthetic edited quietly") {
		t.Fatal("control: the fake has no such message")
	}
	e.fs.SetMutationTimestamps(e.ch, hi, fmt.Sprintf("%d.000100", time.Now().Unix()))
	e.a.refreshCounts(ctx)
	text := func() string {
		if m, ok := e.a.S.Find(e.ch, target); ok {
			return m.Text
		}
		return ""
	}
	e.until("the edited copy", func() bool {
		e.page() // a list reading the conversation
		return strings.Contains(text(), "edited quietly")
	})
	e.log.wait(t, "history_caught_up", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "history_caught_up" && ev.Channel == e.ch {
				return true
			}
		}
		return false
	})
}

// The socket's own change advances the applied history_invalid: counts
// that report it afterwards purge nothing (the history the socket kept
// current stays).
func TestSocketHistoryChangeAdvancesHistoryInvalid(t *testing.T) {
	e := newHCEnv(t)
	ctx := context.Background()
	hi := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	e.fs.SetMutationTimestamps(e.ch, hi, hi)
	e.a.refreshCounts(ctx) // counts applied: the socket's changes advance the timestamps from now
	e.page()               // the first applied history_invalid purged it: cached again
	ev := fakeslack.HistoryChangedEvent("channel_history_changed", e.ch, e.tss[0], false)
	e.fs.Push(ev)
	e.fence()
	if e.a.S.Timeline(e.ch) == nil {
		t.Fatal("control: the history went at the event")
	}
	e.fs.SetMutationTimestamps(e.ch, ev["event_ts"].(string), hi)
	e.a.refreshCounts(ctx)
	if e.a.S.Timeline(e.ch) == nil {
		t.Error("counts reporting the history change the socket brought purged the history again")
	}
}

// baseline applies counts carrying hi as ch's history_invalid and
// updated (the first answer: taken as is), with ch's history cached.
func (e *hcEnv) baseline(hi string) {
	e.t.Helper()
	e.fs.SetMutationTimestamps(e.ch, hi, hi)
	e.a.refreshCounts(context.Background())
	if e.a.S.Timeline(e.ch) == nil {
		e.t.Fatal("the first counts with mutation timestamps purged the history")
	}
}

// The first counts since launch to carry a conversation's mutation
// timestamps are its baseline (Slack's persisted ones, the boot's
// conversations.view): the history just read stays.
func TestCountsFirstAnswerIsBaseline(t *testing.T) {
	e := newHCEnv(t)
	n := e.fs.CallCount("conversations.history")
	e.baseline(fmt.Sprintf("%d.000100", time.Now().Unix()))
	e.page()
	if m := e.fs.CallCount("conversations.history"); m != n {
		t.Errorf("the baseline counts read the history again (%d conversations.history)", m-n)
	}
}

// The applied value the socket advanced while a counts call was out
// stays: an answer older than it doesn't move it back, and the counts
// that report the socket's change purge nothing.
func TestCountsKeepNewerApplied(t *testing.T) {
	e := newHCEnv(t)
	ctx := context.Background()
	old := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	e.baseline(old)
	ev := fakeslack.HistoryChangedEvent("channel_history_changed", e.ch, e.tss[0], false)
	e.fs.Push(ev)
	e.fence()
	e.a.refreshCounts(ctx) // read before the change: the old values
	e.fs.SetMutationTimestamps(e.ch, ev["event_ts"].(string), old)
	e.a.refreshCounts(ctx)
	if e.a.S.Timeline(e.ch) == nil {
		t.Error("counts reporting the change the socket brought purged the history (an older answer moved the applied value back)")
	}
}

// The socket's message_deleted advances the applied history_invalid:
// the counts reporting it purge nothing.
func TestSocketDeleteAdvancesHistoryInvalid(t *testing.T) {
	e := newHCEnv(t)
	ctx := context.Background()
	e.baseline(fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix()))
	target := e.tss[len(e.tss)-2]
	e.fs.DeleteQuiet(e.ch, target)
	evTS := fmt.Sprintf("%d.000200", time.Now().Unix())
	e.fs.Push(map[string]any{"type": "message", "subtype": "message_deleted", "channel": e.ch, "deleted_ts": target, "ts": evTS, "event_ts": evTS})
	e.fence()
	e.fs.SetMutationTimestamps(e.ch, evTS, evTS)
	e.a.refreshCounts(ctx)
	if e.a.S.Timeline(e.ch) == nil {
		t.Error("counts reporting the deletion the socket brought purged the history")
	}
}

// A fast reconnect fetches no counts (fetchAndApplyReconnectCounts: the
// socket replays what it missed), and counts are applied at once.
func TestFastReconnectFetchesNoCounts(t *testing.T) {
	e := newHCEnv(t)
	// a fast reconnect resumes at a reconnect_url the gateway sent
	e.fs.SendReconnectURLs(100 * time.Millisecond)
	t.Cleanup(func() { e.fs.SendReconnectURLs(0) })
	time.Sleep(400 * time.Millisecond)
	n := e.fs.CallCount("client.counts")
	e.fs.DropSockets()
	e.log.wait(t, "a fast reconnect", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "connection" && ev.Connected && ev.Fast {
				return true
			}
		}
		return false
	})
	time.Sleep(500 * time.Millisecond)
	if m := e.fs.CallCount("client.counts"); m != n {
		t.Errorf("a fast reconnect fetched client.counts %d times", m-n)
	}
	h := e.a.histMuts()
	h.mu.Lock()
	applied := h.applied
	h.mu.Unlock()
	if !applied {
		t.Error("after a fast reconnect counts aren't applied: the socket's changes don't advance the timestamps")
	}
}

// A stale mark (the counts' updated) is cleared by any newest-page read
// of the conversation begun after it, not only by the stale read.
func TestFreshReadClearsStale(t *testing.T) {
	e := newHCEnv(t)
	ctx := context.Background()
	other := e.fs.ChannelIDs()[2]
	old := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	e.baseline(old)
	if _, err := e.a.History(ctx, other, ""); err != nil { // another conversation open: ch's read waits for a list
		t.Fatal(err)
	}
	e.fs.SetMutationTimestamps(e.ch, old, fmt.Sprintf("%d.000100", time.Now().Unix()))
	e.a.refreshCounts(ctx)
	if !e.a.historyStale(e.ch) {
		t.Fatal("control: the newer updated didn't mark the history stale")
	}
	if _, err := e.a.Refresh(ctx, e.ch); err != nil {
		t.Fatal(err)
	}
	if e.a.historyStale(e.ch) {
		t.Error("a newest-page read after the mark left it stale")
	}
}

// With no socket (realtime off), the viewer's own delete advances the
// applied history_invalid: the counts that carry it purge nothing.
func TestOwnDeleteAdvancesWithoutSocket(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	ctx := context.Background()
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	ch := fs.ChannelIDs()[1]
	if _, err := a.History(ctx, ch, ""); err != nil {
		t.Fatal(err)
	}
	old := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	fs.SetMutationTimestamps(ch, old, old)
	a.refreshCounts(ctx)
	tss := fs.HistoryTS(ch)
	if err := a.Delete(ctx, ch, tss[len(tss)-2]); err != nil {
		t.Fatal(err)
	}
	fs.SetMutationTimestamps(ch, fmt.Sprintf("%d.000100", time.Now().Unix()), old)
	a.refreshCounts(ctx)
	if a.S.Timeline(ch) == nil {
		t.Error("with no socket, counts carrying the viewer's own delete purged the history")
	}
}
