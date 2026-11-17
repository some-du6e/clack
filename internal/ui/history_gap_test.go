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
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

	// a counts answer that names a newer latest while the list's load is
	// out loads it once more after that lands (the hub's again): the
	// answer was read before the counts
	t.Run("open, counts while its load is out", func(t *testing.T) {
		ch := ids[13]
		e.openAtLatest(ch)
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
	})
}
