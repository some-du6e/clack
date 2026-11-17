package ui

// An app's message posted with its bot token (bot_id, app_id and
// bot_profile, no user, no subtype: Owl's confessions in Hack Club's
// #meta) reaches the channel's list over the socket and stays there.
// Karim on α.102 (macOS): "the newest one isnt there", Activity showing
// Owl's message while the open #meta list ended a message earlier, ~2
// minutes after it came. Slack's socket handler adds the message to its
// store (messageReceived), and a history page merges into the store by
// timestamp (addMessagesFromHistory), so neither a fetch in flight when it
// came nor a later refresh can take it out again. Each case runs with the
// channel open and with another view open while the message came; the
// "in flight" ones answer conversations.history with a page the fake read
// before the message was posted (the reply held back), as a fetch racing
// the socket does. Synthetic data only.

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

const (
	owlBot = "B0LATOWL01"
	owlApp = "A0LATOWL01"
)

// rowDrawn reports whether ch's list drew a row for ts in its last
// layout.
func (e *perfEnv) rowDrawn(ch, ts string) bool {
	c := e.a.convos[ch+"/"]
	if c == nil {
		return false
	}
	l, ok := c.list.(channelList)
	if !ok {
		return false
	}
	for _, r := range l.VisibleRows() {
		if r.Key == ts {
			return true
		}
	}
	return false
}

// owlPost posts Owl's next confession to ch (the fake's history) and
// brings it over the socket; it returns the message's ts.
func owlPost(e *perfEnv, ch string, n int) string {
	ts, ev := e.fs.PostAppQuiet(ch, owlBot, owlApp, "Owl", fmt.Sprintf("%d: Hi meta,", n))
	e.fs.Push(ev)
	return ts
}

// openAtLatest opens ch and scrolls its list to the newest message.
func (e *perfEnv) openAtLatest(ch string) {
	e.openChannel(ch)
	e.until(ch+"'s history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(300 * time.Millisecond)
	e.toLatest()
}

// stays checks that ch's row for ts survives the list's refreshes: the
// one a list on screen makes every 30 s and on every names event (core's
// lookups resolving; refreshVisible) re-read the conversation from core.
func (e *perfEnv) stays(t *testing.T, ch, ts, what string) {
	t.Helper()
	e.ownSendList(ch).Refresh()
	e.settle(400 * time.Millisecond)
	e.a.refreshVisible()
	e.settle(400 * time.Millisecond)
	if !e.rowDrawn(ch, ts) {
		t.Errorf("%s: the message's row is gone after the list re-read the conversation", what)
	}
}

// socketGap posts Owl's next confession to ch while the socket is down
// (the fake's history has it, the socket never brings it), then the
// socket comes back, as after a Mac sleeps, and the counts core reads
// after the reconnect name it ch's latest message. It returns its ts.
func (e *perfEnv) socketGap(t *testing.T, ch string, n int) string {
	t.Helper()
	ts, _ := e.fs.PostAppQuiet(ch, owlBot, owlApp, "Owl", fmt.Sprintf("%d: Hi meta,", n))
	e.fs.SetCountLatest(ch, ts)
	e.a.data.core.RTM.Reconnect("test: the machine woke")
	if !e.untilSoft(20*time.Second, func() bool { return e.a.data.core.Count(ch).Latest == ts && e.fs.Sockets() > 0 }) {
		t.Fatal("the counts read after the reconnect never named the missed message")
	}
	return ts
}

// historyInFlight makes the next conversations.history answer with the
// page the fake has when the call arrives, held back for d: a message
// posted meanwhile is newer than the page.
func historyInFlight(fs *fakeslack.Server, d time.Duration) {
	fs.SetFault("conversations.history", fakeslack.Fault{ReplyDelay: d, Times: 1})
}

func TestBotMessageReachesTheList(t *testing.T) {
	e := newLatEnv(t, latEnvOpts{})
	ids := e.fs.ChannelIDs()
	away := ids[len(ids)-1]
	e.openAtLatest(away)

	t.Run("open", func(t *testing.T) {
		ch := ids[1]
		e.openAtLatest(ch)
		ts := owlPost(e, ch, 42110)
		if !e.untilSoft(5*time.Second, func() bool { return e.rowDrawn(ch, ts) }) {
			t.Fatalf("timed out waiting for %s", "Owl's message in the open list")
		}
		e.stays(t, ch, ts, "open")
		// it becomes a thread parent: three replies come over the socket
		for i := 0; i < 3; i++ {
			u := e.fs.UserIDs()[2+i]
			rts := e.fs.PostReplyQuiet(ch, ts, u, fmt.Sprintf("reply %d", i))
			e.fs.Push(map[string]any{"type": "message", "channel": ch, "user": u, "text": fmt.Sprintf("reply %d", i), "ts": rts, "thread_ts": ts, "event_ts": rts})
			e.fs.Push(map[string]any{"type": "message", "subtype": "message_replied", "hidden": true, "channel": ch, "event_ts": rts,
				"message": map[string]any{"type": "message", "ts": ts, "thread_ts": ts, "bot_id": owlBot, "app_id": owlApp, "text": "42110: Hi meta,",
					"reply_count": i + 1, "reply_users": []string{e.fs.UserIDs()[2]}, "reply_users_count": 1, "latest_reply": rts}})
		}
		e.settle(600 * time.Millisecond)
		if !e.rowDrawn(ch, ts) {
			t.Error("the parent's row is gone after its replies came")
		}
		e.stays(t, ch, ts, "open, a thread parent")
	})

	t.Run("open, its history in flight", func(t *testing.T) {
		ch := ids[2]
		historyInFlight(e.fs, 800*time.Millisecond)
		e.openChannel(ch)
		e.settle(100 * time.Millisecond) // the page is read; its answer waits
		ts := owlPost(e, ch, 42111)
		if !e.untilSoft(20*time.Second, func() bool { return e.loadedMessages(ch) > 0 }) {
			t.Fatal("no history")
		}
		e.toLatest()
		if !e.untilSoft(5*time.Second, func() bool { return e.rowDrawn(ch, ts) }) {
			t.Fatalf("timed out waiting for %s", "Owl's message in the list")
		}
		e.stays(t, ch, ts, "open, its history in flight")
	})

	t.Run("away, then reopened", func(t *testing.T) {
		ch := ids[3]
		e.openAtLatest(ch)
		e.openAtLatest(away)
		ts := owlPost(e, ch, 42112)
		e.settle(300 * time.Millisecond)
		e.openAtLatest(ch)
		if !e.untilSoft(5*time.Second, func() bool { return e.rowDrawn(ch, ts) }) {
			t.Fatalf("timed out waiting for %s", "Owl's message in the reopened list")
		}
		e.stays(t, ch, ts, "away, then reopened")
	})

	t.Run("open, the socket down when it came", func(t *testing.T) {
		ch := ids[5]
		e.openAtLatest(ch)
		ts := e.socketGap(t, ch, 42114)
		if !e.untilSoft(8*time.Second, func() bool { return e.rowDrawn(ch, ts) }) {
			t.Fatal("the missed message never reached the open list")
		}
		e.stays(t, ch, ts, "open, the socket down when it came")
	})

	t.Run("away, the socket down when it came", func(t *testing.T) {
		ch := ids[6]
		e.openAtLatest(ch)
		e.openAtLatest(away)
		ts := e.socketGap(t, ch, 42115)
		e.openAtLatest(ch)
		if !e.untilSoft(5*time.Second, func() bool { return e.rowDrawn(ch, ts) }) {
			t.Fatal("the missed message isn't in the reopened list")
		}
		e.stays(t, ch, ts, "away, the socket down when it came")
	})

	t.Run("away, its history read as it came", func(t *testing.T) {
		// a channel not opened yet whose newest page is being read (the
		// sidebar row's pointerdown prefetch, boot's unread prefetch) when
		// Owl posts
		ch := ids[4]
		historyInFlight(e.fs, 800*time.Millisecond)
		if !e.a.data.core.PrefetchHistory(ch) {
			t.Fatal("no prefetch started")
		}
		time.Sleep(150 * time.Millisecond) // the page is read; its answer waits
		ts := owlPost(e, ch, 42113)
		if !e.untilSoft(10*time.Second, func() bool { return e.loadedMessages(ch) > 0 }) {
			t.Fatal("the prefetched page never came")
		}
		e.openAtLatest(ch)
		if !e.untilSoft(5*time.Second, func() bool { return e.rowDrawn(ch, ts) }) {
			t.Fatalf("timed out waiting for %s", "Owl's message in the list")
		}
		e.stays(t, ch, ts, "away, its history read as it came")
	})
}
