package ui

// What the socket never brought reaches the lists, however many messages
// it brought since. Slack's views read Slack again after a connect that
// wasn't fast (the reachedEnd reset in its onConnected handler), and
// client.counts' history_invalid and updated purge or invalidate a cached
// history the socket said nothing about (maybePurgeChannelHistoryAfterMutations).
// Clack's catch-up compared only the newest message with the counts'
// latest, so a gap stayed for the session once one more message came over
// the socket (Karim's "the newest one isnt there" after his Mac slept).
// Every case holds the counts core reads after the reconnect back until the
// socket brought a later message: the window's newest is the latest by
// the time they land. core/history_catchup.go. Synthetic data only.

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/store"
)

// rowText is the text ch's list draws for ts, and whether it drew the
// row in its last layout.
func (e *perfEnv) rowText(ch, ts string) (string, bool) {
	c := e.a.convos[ch+"/"]
	if c == nil {
		return "", false
	}
	l, ok := c.list.(channelList)
	if !ok {
		return "", false
	}
	text, _, ok := l.RowText(ts)
	return text, ok && e.rowDrawn(ch, ts)
}

// threadRow reports whether the open thread ch/root drew a row for ts.
func (e *perfEnv) threadRow(ch, root, ts string) bool {
	tv := e.threadView(ch, root)
	if tv == nil {
		return false
	}
	_, _, ok := tv.RowText(ts)
	return ok
}

var gapProbeN atomic.Int64

// countsProbe makes the counts name a new latest for probe, a
// conversation nothing opens, and returns a func that reports whether
// counts read after this call landed in core.
func (e *perfEnv) countsProbe(probe string) func() bool {
	ts := fmt.Sprintf("1800000000.%06d", gapProbeN.Add(1))
	e.fs.SetCountLatest(probe, ts)
	return func() bool { return e.a.data.core.Count(probe).Latest == ts }
}

// countsSoon makes core read client.counts within about 1.5 s, as a
// thread reply from someone else does (refreshCountsSoon): a reply in ch
// under a root nobody has open.
func (e *perfEnv) countsSoon(ch string) {
	u := e.fs.UserIDs()[3]
	ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), 900+gapProbeN.Add(1)%99)
	e.fs.Push(map[string]any{"type": "message", "channel": ch, "user": u, "text": "nudge", "ts": ts,
		"thread_ts": "1700000000.000100", "event_ts": ts})
}

// gapThenMore posts Owl's next confession to ch while the socket is down,
// brings the socket back with the counts held, and posts the following
// confession over the new socket before they land. It returns both ts.
func (e *perfEnv) gapThenMore(t *testing.T, ch, probe string, n int) (missed, next string) {
	t.Helper()
	missed, _ = e.fs.PostAppQuiet(ch, owlBot, owlApp, "Owl", fmt.Sprintf("%d: Hi meta,", n))
	e.fs.SetCountLatest(ch, missed)
	landed := e.countsProbe(probe)
	e.reconnectSlow(t, 1500*time.Millisecond)
	next = owlPost(e, ch, n+1)
	svc := e.a.data.core
	if !e.untilSoft(10*time.Second, func() bool { return svc.Count(ch).Latest == next }) {
		t.Fatal("the message after the reconnect never reached core")
	}
	if !e.untilSoft(20*time.Second, landed) {
		t.Fatal("the counts read after the reconnect never landed")
	}
	return missed, next
}

func nowSlackTS() string {
	now := time.Now()
	return fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
}

func TestHistoryAfterSocketGap(t *testing.T) {
	e := newLatEnv(t, latEnvOpts{})
	ids := e.fs.ChannelIDs()
	away, probe, nudge := ids[len(ids)-1], ids[len(ids)-2], ids[len(ids)-3]
	// the mutation timestamps the counts carry for the channels the
	// purge and invalidate cases use, applied before any window of
	// theirs is read: their next move is the first the client misses
	before := "1794000000.000100"
	for _, ch := range ids[10:13] {
		e.fs.SetCountMutations(ch, before, before)
	}
	landed := e.countsProbe(probe)
	e.countsSoon(nudge)
	e.until("the counts", 15*time.Second, landed)
	e.openAtLatest(away)

	t.Run("open, a message after the reconnect", func(t *testing.T) {
		ch := ids[7]
		e.openAtLatest(ch)
		missed, next := e.gapThenMore(t, ch, probe, 42120)
		if !e.untilSoft(8*time.Second, func() bool { return e.rowDrawn(ch, missed) && e.rowDrawn(ch, next) }) {
			t.Fatalf("the open list draws the missed message %v and the next one %v", e.rowDrawn(ch, missed), e.rowDrawn(ch, next))
		}
		e.stays(t, ch, missed, "open, a message after the reconnect")
	})

	t.Run("away, a message after the reconnect", func(t *testing.T) {
		ch := ids[8]
		e.openAtLatest(ch)
		e.openAtLatest(away)
		missed, next := e.gapThenMore(t, ch, probe, 42122)
		e.openAtLatest(ch)
		if !e.untilSoft(8*time.Second, func() bool { return e.rowDrawn(ch, missed) && e.rowDrawn(ch, next) }) {
			t.Fatalf("the reopened list draws the missed message %v and the next one %v", e.rowDrawn(ch, missed), e.rowDrawn(ch, next))
		}
		e.stays(t, ch, missed, "away, a message after the reconnect")
	})

	t.Run("an open thread, a reply after the reconnect", func(t *testing.T) {
		ch := ids[9]
		e.openAtLatest(ch)
		root := owlPost(e, ch, 42124)
		u := e.fs.UserIDs()[2]
		reply := func(text string, push bool) string {
			rts := e.fs.PostReplyQuiet(ch, root, u, text)
			if push {
				e.fs.Push(map[string]any{"type": "message", "channel": ch, "user": u, "text": text, "ts": rts, "thread_ts": root, "event_ts": rts})
			}
			return rts
		}
		first := reply("before the gap", true)
		e.a.OpenThread(ch, root)
		if !e.untilSoft(15*time.Second, func() bool { return e.threadRow(ch, root, first) }) {
			t.Fatal("the thread never drew its reply")
		}
		missed := reply("while the socket was down", false)
		landed := e.countsProbe(probe)
		e.reconnectSlow(t, 1500*time.Millisecond)
		next := reply("after the reconnect", true)
		if !e.untilSoft(20*time.Second, landed) {
			t.Fatal("the counts read after the reconnect never landed")
		}
		if !e.untilSoft(8*time.Second, func() bool { return e.threadRow(ch, root, missed) && e.threadRow(ch, root, next) }) {
			t.Fatalf("the open thread draws the missed reply %v and the next one %v", e.threadRow(ch, root, missed), e.threadRow(ch, root, next))
		}
	})

	// client.counts' history_invalid past the one last applied: the
	// history and its threads are purged (a deletion the socket never
	// told)
	t.Run("open, a deletion the socket missed", func(t *testing.T) {
		ch := ids[10]
		e.openAtLatest(ch)
		victim := owlPost(e, ch, 42126)
		if !e.untilSoft(5*time.Second, func() bool { return e.rowDrawn(ch, victim) }) {
			t.Fatal("Owl's message never drew")
		}
		e.fs.DeleteQuiet(ch, victim)
		e.fs.SetCountMutations(ch, nowSlackTS(), before)
		landed := e.countsProbe(probe)
		e.countsSoon(nudge)
		if !e.untilSoft(10*time.Second, landed) {
			t.Fatal("no counts")
		}
		if !e.untilSoft(8*time.Second, func() bool { return !e.rowDrawn(ch, victim) && e.loadedMessages(ch) > 0 }) {
			t.Fatal("the open list still draws the message deleted while nothing told the client")
		}
	})

	t.Run("away, a deletion the socket missed", func(t *testing.T) {
		ch := ids[11]
		e.openAtLatest(ch)
		victim := owlPost(e, ch, 42127)
		if !e.untilSoft(5*time.Second, func() bool { return e.rowDrawn(ch, victim) }) {
			t.Fatal("Owl's message never drew")
		}
		e.openAtLatest(away)
		e.fs.DeleteQuiet(ch, victim)
		e.fs.SetCountMutations(ch, nowSlackTS(), before)
		landed := e.countsProbe(probe)
		e.countsSoon(nudge)
		if !e.untilSoft(10*time.Second, landed) {
			t.Fatal("no counts")
		}
		e.openAtLatest(ch)
		e.settle(600 * time.Millisecond)
		if e.rowDrawn(ch, victim) {
			t.Fatal("the reopened list draws the message deleted while nothing told the client")
		}
	})

	// updated past the one last applied: the history is read again (an
	// edit the socket never told)
	t.Run("away, an edit the socket missed", func(t *testing.T) {
		ch := ids[12]
		e.openAtLatest(ch)
		target := owlPost(e, ch, 42128)
		if !e.untilSoft(5*time.Second, func() bool { return e.rowDrawn(ch, target) }) {
			t.Fatal("Owl's message never drew")
		}
		e.openAtLatest(away)
		e.fs.EditQuiet(ch, target, "42128: edited while nobody listened")
		e.fs.SetCountMutations(ch, before, nowSlackTS())
		landed := e.countsProbe(probe)
		e.countsSoon(nudge)
		if !e.untilSoft(10*time.Second, landed) {
			t.Fatal("no counts")
		}
		e.openAtLatest(ch)
		if !e.untilSoft(8*time.Second, func() bool {
			text, ok := e.rowText(ch, target)
			return ok && strings.Contains(text, "edited while nobody listened")
		}) {
			text, _ := e.rowText(ch, target)
			t.Fatalf("the reopened list draws %q, not the edit", text)
		}
	})

	// a counts answer that names a newer latest while the catch-up's read
	// is out still reaches the list, through one more read in that
	// catch-up (core's catchUpOnce): it lands once, with both messages.
	// Without it the catch-up lands with the held page (the first message
	// alone), and only the list's reload after it starts another. (The
	// hub's again, for a load the connection event came during, is
	// TestBotMessageReachesTheList's "open, the socket down when it came".)
	t.Run("open, counts while its catch-up is out", func(t *testing.T) {
		ch := ids[13]
		e.openAtLatest(ch)
		evs, stop := e.a.data.core.Subscribe()
		var landings atomic.Int32
		watched := make(chan struct{})
		go func() {
			defer close(watched)
			for ev := range evs {
				if ev.Type == "history_caught_up" && ev.Channel == ch && ev.ThreadTS == "" {
					landings.Add(1)
				}
			}
		}()
		defer func() {
			stop()
			<-watched
		}()
		first, _ := e.fs.PostAppQuiet(ch, owlBot, owlApp, "Owl", "42130: Hi meta,")
		e.fs.SetCountLatest(ch, first)
		read := historyInFlight(e.fs, ch, 4*time.Second)
		landed := e.countsProbe(probe)
		e.countsSoon(nudge) // the list catches up with first: its page is held
		if !e.untilSoft(10*time.Second, func() bool { return landed() && read() }) {
			t.Fatalf("counts %v, the catch-up's page read %v", landed(), read())
		}
		second, _ := e.fs.PostAppQuiet(ch, owlBot, owlApp, "Owl", "42131: Hi meta,")
		e.fs.SetCountLatest(ch, second)
		landed = e.countsProbe(probe)
		e.countsSoon(nudge) // while the held page is out
		if !e.untilSoft(10*time.Second, landed) {
			t.Fatal("no counts")
		}
		if !e.untilSoft(10*time.Second, func() bool { return e.rowDrawn(ch, first) && e.rowDrawn(ch, second) }) {
			t.Fatalf("the open list draws the first %v and the second %v", e.rowDrawn(ch, first), e.rowDrawn(ch, second))
		}
		e.settle(500 * time.Millisecond)
		if got := landings.Load(); got != 1 {
			t.Errorf("the catch-up landed %d times, want once: its page was the held one, and another catch-up followed", got)
		}
	})
}

// listTS is what ch's list holds, oldest first.
func (e *perfEnv) listTS(ch string) []string {
	e.frame()
	return e.ownSendList(ch).LoadedTS()
}

// scrollBack scrolls ch's list toward its oldest message until it holds
// at least n messages (older pages load as it nears the top).
func (e *perfEnv) scrollBack(t *testing.T, ch string, n int) {
	t.Helper()
	if !e.untilSoft(30*time.Second, func() bool {
		e.scroll(e.listPoint(), -3000)
		e.settle(60 * time.Millisecond)
		return len(e.listTS(ch)) >= n
	}) {
		t.Fatalf("the list holds %d messages after scrolling back, want %d", len(e.listTS(ch)), n)
	}
}

// noHole fails when the list draws a message from before the gap followed
// by one from after it while a missed message between them is left out:
// a catch-up page that doesn't reach the list's messages must not be
// merged under them (Slack keeps it a slice of its own, and loads back
// from its start).
func noHole(t *testing.T, what string, list []string, missed []string) {
	t.Helper()
	held := map[string]bool{}
	for _, ts := range list {
		held[ts] = true
	}
	first := missed[0]
	for i := 1; i < len(list); i++ {
		a, b := list[i-1], list[i]
		if !store.TSLess(a, first) || store.TSLess(b, first) {
			continue // not the boundary between before and after the gap
		}
		for _, m := range missed {
			if store.TSLess(a, m) && store.TSLess(m, b) && !held[m] {
				t.Fatalf("%s: the list draws %s, then %s, without the missed %s between them", what, a, b, m)
			}
		}
	}
}

// A catch-up after a gap of more than one page (an overnight sleep in a
// busy channel) never leaves a hole in the list, and the list's
// scrollback survives a catch-up whose page reaches it.
func TestHistoryAfterLongGap(t *testing.T) {
	e := newLatEnv(t, latEnvOpts{})
	ids := e.fs.ChannelIDs()
	probe := ids[len(ids)-2]

	t.Run("open, a gap of more than a page", func(t *testing.T) {
		ch := ids[14]
		e.openAtLatest(ch)
		var missed []string
		for i := 0; i < 60; i++ {
			ts, _ := e.fs.PostAppQuiet(ch, owlBot, owlApp, "Owl", fmt.Sprintf("%d: missed overnight", 43000+i))
			missed = append(missed, ts)
		}
		e.fs.SetCountLatest(ch, missed[len(missed)-1])
		landed := e.countsProbe(probe)
		e.reconnectSlow(t, 0)
		if !e.untilSoft(20*time.Second, landed) {
			t.Fatal("no counts after the reconnect")
		}
		newest := missed[len(missed)-1]
		if !e.untilSoft(10*time.Second, func() bool { return e.rowDrawn(ch, newest) }) {
			t.Fatal("the newest missed message never drew")
		}
		e.settle(400 * time.Millisecond)
		noHole(t, "after the catch-up", e.listTS(ch), missed)
		// scrolling back reaches every missed message, then what came before
		e.scrollBack(t, ch, len(e.listTS(ch))+40)
		list := e.listTS(ch)
		noHole(t, "scrolled back", list, missed)
		held := map[string]bool{}
		for _, ts := range list {
			held[ts] = true
		}
		for _, m := range missed {
			if !held[m] && !store.TSLess(m, list[0]) {
				t.Fatalf("scrolled back past the gap, the list never got the missed %s", m)
			}
		}
	})

	t.Run("open, scrolled back, then a gap", func(t *testing.T) {
		ch := ids[15]
		// a long history: the list holds more than one page after
		// scrolling, and core more than the catch-up page
		for i := 0; i < 160; i++ {
			e.fs.PostQuiet(ch, e.fs.UserIDs()[2+i%5], fmt.Sprintf("earlier %d", i))
		}
		e.openAtLatest(ch)
		e.scrollBack(t, ch, 140)
		read := e.listTS(ch)
		missed, _ := e.fs.PostAppQuiet(ch, owlBot, owlApp, "Owl", "43100: Hi meta,")
		e.fs.SetCountLatest(ch, missed)
		landed := e.countsProbe(probe)
		e.reconnectSlow(t, 0)
		if !e.untilSoft(20*time.Second, landed) {
			t.Fatal("no counts after the reconnect")
		}
		e.settle(600 * time.Millisecond)
		// further back, then the list's own refresh (every 30 s on screen)
		e.scrollBack(t, ch, len(e.listTS(ch))+60)
		e.ownSendList(ch).Refresh()
		e.settle(800 * time.Millisecond)
		after := map[string]bool{}
		for _, ts := range e.listTS(ch) {
			after[ts] = true
		}
		for _, ts := range read {
			if !after[ts] {
				t.Fatalf("the list lost %s, which it held before the gap (the user was reading it)", ts)
			}
		}
	})
}

// While the catch-up after a wake is out, the list doesn't draw what the
// socket brought since straight after what it held before the gap: Slack
// renders the newest slice alone after resetReachedEnd, and loads back
// from its start. The list drew yesterday's last message directly above
// today's, with the gap between them unmarked, until the catch-up landed
// (or for good, if it failed).
func TestNoHoleWhileCatchingUp(t *testing.T) {
	e := newLatEnv(t, latEnvOpts{})
	ids := e.fs.ChannelIDs()
	ch, probe := ids[16], ids[len(ids)-2]
	e.openAtLatest(ch)
	list := e.listTS(ch)
	if len(list) == 0 {
		t.Fatal("an empty list")
	}
	pre := list[len(list)-1] // the newest message before the gap
	var missed []string
	for i := 0; i < 5; i++ {
		ts, _ := e.fs.PostAppQuiet(ch, owlBot, owlApp, "Owl", fmt.Sprintf("%d: while asleep", 43200+i))
		missed = append(missed, ts)
	}
	read := historyInFlight(e.fs, ch, 3*time.Second) // the catch-up's read is slow
	landed := e.countsProbe(probe)
	e.reconnectSlow(t, 0)
	next := owlPost(e, ch, 43210) // the socket brings one after the wake
	if !e.untilSoft(10*time.Second, func() bool { return e.rowDrawn(ch, next) && read() }) {
		t.Fatalf("the post-gap message drew %v, the catch-up's read is out %v", e.rowDrawn(ch, next), read())
	}
	e.settle(300 * time.Millisecond)
	if e.rowDrawn(ch, pre) {
		t.Errorf("while the catch-up was out the list drew the pre-gap %s straight above the post-gap %s", pre, next)
	}
	if !e.untilSoft(20*time.Second, func() bool { return landed() && e.rowDrawn(ch, missed[len(missed)-1]) }) {
		t.Fatal("the catch-up never brought the missed messages")
	}
	e.settle(400 * time.Millisecond)
	noHole(t, "after the catch-up", e.listTS(ch), missed)
}

// An older page out when the window starts over belongs to the old one:
// the list drops it instead of putting it on top of the new page, and
// loads back from the new page's start. It drew the stale page above the
// catch-up, with the overnight messages missing between them.
func TestOlderPageAcrossRestartInList(t *testing.T) {
	e := newLatEnv(t, latEnvOpts{})
	ids := e.fs.ChannelIDs()
	ch, probe := ids[17], ids[len(ids)-2]
	for i := 0; i < 160; i++ {
		e.fs.PostQuiet(ch, e.fs.UserIDs()[2+i%5], fmt.Sprintf("earlier %d", i))
	}
	e.openAtLatest(ch)
	e.fs.SetChannelFault("conversations.history", ch, fakeslack.Fault{ReplyDelay: 2500 * time.Millisecond, Times: 1})
	n := e.fs.HistoryReads(ch)
	if !e.untilSoft(10*time.Second, func() bool {
		e.scroll(e.listPoint(), -3000) // the reader scrolls back
		e.settle(40 * time.Millisecond)
		return e.fs.HistoryReads(ch) > n
	}) {
		t.Fatal("scrolling back never asked for an older page")
	}
	var missed []string
	for i := 0; i < 60; i++ {
		ts, _ := e.fs.PostAppQuiet(ch, owlBot, owlApp, "Owl", fmt.Sprintf("%d: missed overnight", 43300+i))
		missed = append(missed, ts)
	}
	e.fs.SetCountLatest(ch, missed[len(missed)-1])
	landed := e.countsProbe(probe)
	e.reconnectSlow(t, 0)
	if !e.untilSoft(20*time.Second, func() bool { return landed() && e.rowDrawn(ch, missed[len(missed)-1]) }) {
		t.Fatal("the catch-up never drew the newest missed message")
	}
	e.settle(3 * time.Second) // the old window's older page lands
	noHole(t, "after the old window's older page landed", e.listTS(ch), missed)
}

// A failed send left below a window that started over is no anchor for
// paging back: the list reads the older page from the window's oldest
// message from Slack, not from the failed copy's local ts (kept whatever
// the range, with its Retry). From there it skipped every message between
// the two, and paging back never filled the hole.
func TestOlderPageFromSlacksOldestNotAFailedSend(t *testing.T) {
	e := newLatEnv(t, latEnvOpts{})
	ids := e.fs.ChannelIDs()
	ch, probe := ids[16], ids[len(ids)-2]
	for i := 0; i < 160; i++ {
		e.fs.PostQuiet(ch, e.fs.UserIDs()[2+i%5], fmt.Sprintf("earlier %d", i))
	}
	e.openAtLatest(ch)
	e.fs.SetFault("chat.postMessage", fakeslack.Fault{Error: "fatal_error", Times: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := e.a.data.core.Send(ctx, core.SendOpts{Channel: ch, Text: "the send that failed"}); err == nil {
		t.Fatal("the send went out")
	}
	// more than a page while the socket was down, then the wake: the
	// window starts over at the newest page, the failed copy below it
	var missed []string
	for i := 0; i < 60; i++ {
		ts, _ := e.fs.PostAppQuiet(ch, owlBot, owlApp, "Owl", fmt.Sprintf("%d: missed overnight", 43400+i))
		missed = append(missed, ts)
	}
	e.fs.SetCountLatest(ch, missed[len(missed)-1])
	landed := e.countsProbe(probe)
	e.reconnectSlow(t, 0)
	if !e.untilSoft(20*time.Second, func() bool { return landed() && e.rowDrawn(ch, missed[len(missed)-1]) }) {
		t.Fatal("the catch-up never drew the newest missed message")
	}
	e.settle(400 * time.Millisecond)
	e.scrollBack(t, ch, 90) // past the failed send
	noHole(t, "after paging back past the failed send", e.listTS(ch), missed)
}
