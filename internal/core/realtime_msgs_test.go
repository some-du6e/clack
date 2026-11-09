package core

import (
	"context"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Realtime message state through the fake's socket: sends and their
// echoes, edits, deleted replies, tombstones and broadcasts, in the shapes
// Slack's gateway sends them.

// rtApp boots a client with the realtime socket up; evs sees every event
// emitted after the connection.
func rtApp(t *testing.T) (*App, *fakeslack.Server, *eventLog) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	evs, stop := a.Subscribe()
	t.Cleanup(stop)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	l := &eventLog{}
	go func() {
		for ev := range evs {
			l.add(ev)
		}
	}()
	return a, fs, l
}

type eventLog struct {
	mu  sync.Mutex
	evs []Event
}

func (l *eventLog) add(ev Event) {
	l.mu.Lock()
	l.evs = append(l.evs, ev)
	l.mu.Unlock()
}

func (l *eventLog) all() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Event(nil), l.evs...)
}

// wait polls until cond holds over the log (or fails after 5s).
func (l *eventLog) wait(t *testing.T, what string, cond func([]Event) bool) {
	t.Helper()
	dl := time.Now().Add(5 * time.Second)
	for !cond(l.all()) {
		if time.Now().After(dl) {
			t.Fatalf("timed out waiting for %s; events: %s", what, evTypes(l.all()))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func evTypes(evs []Event) string {
	s := ""
	for _, ev := range evs {
		s += ev.Type + "(" + ev.TS + ") "
	}
	return s
}

func countWhere(msgs []slack.Message, f func(*slack.Message) bool) int {
	n := 0
	for i := range msgs {
		if f(&msgs[i]) {
			n++
		}
	}
	return n
}

func timelineOf(a *App, ch string) []slack.Message {
	if tl := a.S.Timeline(ch); tl != nil {
		return tl.Msgs
	}
	return nil
}

// send → echo → exactly one message, whichever of the echo and the
// chat.postMessage reply lands first, in a channel and in a thread.
func TestRealtimeSendEchoOneMessage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		echoFirst  bool
		threadRoot bool
	}{
		{"echo after reply", false, false},
		{"echo before reply", true, false},
		{"thread reply, echo after", false, true},
		{"thread reply, echo before", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, fs, log := rtApp(t)
			ch := fs.ChannelIDs()[1]
			if _, err := a.History(context.Background(), ch, ""); err != nil {
				t.Fatal(err)
			}
			parent := ""
			if tc.threadRoot {
				parent = timelineOf(a, ch)[len(timelineOf(a, ch))-1].TS
				a.S.SetThread(ch, parent, []slack.Message{{Type: "message", TS: parent, ThreadTS: parent, User: "U0FAKE00001", Text: "root"}})
			}
			echo := func(msg map[string]any) {
				ev := map[string]any{"type": "message", "channel": ch, "event_ts": msg["ts"]}
				for k, v := range msg {
					ev[k] = v
				}
				fs.Push(ev)
			}
			var mu sync.Mutex
			var posted map[string]any
			fs.OnPost = func(_ url.Values, msg map[string]any) {
				mu.Lock()
				posted = msg
				mu.Unlock()
				if tc.echoFirst {
					echo(msg)
					time.Sleep(50 * time.Millisecond) // let the echo be applied before the reply
				}
			}
			text := "hello from " + tc.name
			got, err := a.Send(context.Background(), SendOpts{Channel: ch, ThreadTS: parent, Text: text})
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			msg := posted
			mu.Unlock()
			if !tc.echoFirst {
				echo(msg)
			}
			// the echo is applied once an update (late echo) or the sent event for its ts arrived
			log.wait(t, "echo applied", func(evs []Event) bool {
				n := 0
				for _, ev := range evs {
					if ev.TS == got.TS && (ev.Type == "sent" || ev.Type == "update") {
						n++
					}
				}
				return n >= 2 || tc.echoFirst && n >= 1
			})
			time.Sleep(50 * time.Millisecond)
			list := timelineOf(a, ch)
			if tc.threadRoot {
				list, _ = a.S.Thread(ch, parent)
			}
			if n := countWhere(list, func(m *slack.Message) bool { return m.Text == text }); n != 1 {
				t.Fatalf("%d copies of the sent message, want 1: %+v", n, list)
			}
			if tc.threadRoot {
				if n := countWhere(timelineOf(a, ch), func(m *slack.Message) bool { return m.Text == text }); n != 0 {
					t.Errorf("the thread reply also shows in the channel (%d)", n)
				}
				if p, _ := a.S.Find(ch, parent); p.ReplyCount != 1 {
					t.Errorf("parent reply_count %d, want 1", p.ReplyCount)
				}
			}
			msgs, sent := 0, 0
			for _, ev := range log.all() {
				if ev.Msg != nil && ev.Msg.Text == text {
					switch ev.Type {
					case "message":
						msgs++
						if !ev.Msg.Pending {
							t.Errorf("a second, non-pending message event for the send (ts %s)", ev.TS)
						}
					case "sent":
						sent++
					}
				}
			}
			// one "message" (the pending copy), one "sent" (the swap), however the echo raced
			if msgs != 1 || sent != 1 {
				t.Errorf("events: %d message, %d sent; want 1 and 1 (%s)", msgs, sent, evTypes(log.all()))
			}
			if _, local := a.LocalTS(got.TS); local {
				t.Error("the confirmed ts counts as a local one")
			}
		})
	}
}

// fixtureCh is a channel the fake doesn't know, so no prefetch or
// history load replaces the fixture's cached timeline.
const fixtureCh = "C0TESTFIX01"

// threadFixture caches a parent with two replies in ch's timeline and its thread.
func threadFixture(a *App, ch string) (parent, r1, r2 slack.Message) {
	now := time.Now().Unix() - 60
	ts := func(d int64) string { return fmtTS(now + d) }
	parent = slack.Message{Type: "message", TS: ts(0), ThreadTS: ts(0), User: "U0FAKE00001", Text: "root",
		ReplyCount: 2, ReplyUsers: []string{"U0FAKE00002"}, LatestReply: ts(2)}
	r1 = slack.Message{Type: "message", TS: ts(1), ThreadTS: ts(0), User: "U0FAKE00002", Text: "one"}
	r2 = slack.Message{Type: "message", TS: ts(2), ThreadTS: ts(0), User: "U0FAKE00002", Text: "two"}
	a.S.SetLatest(ch, []slack.Message{parent}, false)
	a.S.SetThread(ch, parent.TS, []slack.Message{parent, r1, r2})
	return
}

func fmtTS(sec int64) string { return strconv.FormatInt(sec, 10) + ".000100" }

// Slack's gateway for a deleted reply: message_deleted for the reply (with
// its previous_message), then message_changed for the parent with the new
// reply metadata; after the last reply the parent has none at all.
func pushDeleteReply(fs *fakeslack.Server, ch string, reply slack.Message, parent map[string]any) {
	fs.Push(map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "channel": ch,
		"deleted_ts": reply.TS, "event_ts": reply.TS,
		"previous_message": map[string]any{"type": "message", "ts": reply.TS, "thread_ts": reply.ThreadTS, "user": reply.User, "text": reply.Text}})
	fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch, "event_ts": reply.TS,
		"message": parent})
}

// Deleting thread replies: the parent's count drops (to no reply bar after
// the last one) and nothing shows up in the channel.
func TestRealtimeDeleteThreadReply(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fixtureCh // the fake serves no history for it: nothing refetches over the fixture
	p, r1, r2 := threadFixture(a, ch)

	pushDeleteReply(fs, ch, r2, map[string]any{"type": "message", "ts": p.TS, "thread_ts": p.TS, "user": p.User, "text": p.Text,
		"reply_count": 1, "reply_users": []string{r1.User}, "reply_users_count": 1, "latest_reply": r1.TS})
	log.wait(t, "parent at 1 reply", func([]Event) bool { m, _ := a.S.Find(ch, p.TS); return m.ReplyCount == 1 })
	pushDeleteReply(fs, ch, r1, map[string]any{"type": "message", "ts": p.TS, "user": p.User, "text": p.Text})
	log.wait(t, "parent without replies", func([]Event) bool {
		m, _ := a.S.Find(ch, p.TS)
		return m.ReplyCount == 0 && m.LatestReply == ""
	})
	if tl := timelineOf(a, ch); len(tl) != 1 || tl[0].TS != p.TS {
		t.Errorf("channel timeline %+v, want only the parent", tl)
	}
	th, _ := a.S.Thread(ch, p.TS)
	for _, m := range th {
		if m.TS == r1.TS || m.TS == r2.TS {
			t.Errorf("deleted reply %s still in the thread", m.TS)
		}
	}
	dels := 0
	for _, ev := range log.all() {
		if ev.Type == "delete" {
			dels++
			if ev.ThreadTS != p.TS {
				t.Errorf("delete of %s has ThreadTS %q, want the parent's", ev.TS, ev.ThreadTS)
			}
		}
		if ev.Type == "message" && ev.Msg != nil && (ev.TS == r1.TS || ev.TS == r2.TS) {
			t.Errorf("a deleted reply was announced as a new message")
		}
	}
	if dels != 2 {
		t.Errorf("%d delete events, want 2", dels)
	}
	if v := a.message(ch, &timelineOf(a, ch)[0]); v.ReplyCount != 0 {
		t.Errorf("UI parent reply count %d", v.ReplyCount)
	}
}

// An edited reply whose thread isn't cached stays out of the channel.
func TestRealtimeEditReplyUncachedThread(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fixtureCh // the fake serves no history for it: nothing refetches over the fixture
	p, _, r2 := threadFixture(a, ch)
	a.S.SetThread(ch, p.TS, nil) // the thread's replies aren't cached
	fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch, "event_ts": r2.TS,
		"message": map[string]any{"type": "message", "ts": r2.TS, "thread_ts": p.TS, "user": r2.User, "text": "two (edited)",
			"edited": map[string]any{"user": r2.User, "ts": r2.TS}}})
	log.wait(t, "update", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "update" && ev.TS == r2.TS {
				return true
			}
		}
		return false
	})
	if tl := timelineOf(a, ch); len(tl) != 1 {
		t.Errorf("channel timeline has %d messages after a reply edit, want 1: %+v", len(tl), tl)
	}
	// a top-level edit replaces in place
	fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch, "event_ts": p.TS,
		"message": map[string]any{"type": "message", "ts": p.TS, "thread_ts": p.TS, "user": p.User, "text": "root (edited)",
			"reply_count": 2, "reply_users": []string{r2.User}, "latest_reply": r2.TS, "edited": map[string]any{"user": p.User, "ts": p.TS}}})
	log.wait(t, "parent edited", func([]Event) bool { m, _ := a.S.Find(ch, p.TS); return m.Text == "root (edited)" })
	if tl := timelineOf(a, ch); len(tl) != 1 || tl[0].ReplyCount != 2 {
		t.Errorf("timeline after edit %+v", tl)
	}
}

// A deleted parent with replies becomes Slack's tombstone (message_changed
// to subtype "tombstone", user USLACKBOT, hidden): one row, flagged
// Tombstone for the UI, with its replies; deleting the last reply then
// removes it (message_deleted).
func TestRealtimeTombstone(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fixtureCh // the fake serves no history for it: nothing refetches over the fixture
	p, r1, r2 := threadFixture(a, ch)
	fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch, "event_ts": p.TS,
		"message": map[string]any{"type": "message", "subtype": "tombstone", "hidden": true, "ts": p.TS, "thread_ts": p.TS,
			"user": "USLACKBOT", "text": "This message was deleted.", "reply_count": 2, "reply_users": []string{r1.User}, "latest_reply": r2.TS},
		"previous_message": map[string]any{"type": "message", "ts": p.TS, "thread_ts": p.TS, "user": p.User, "text": p.Text}})
	log.wait(t, "tombstone", func([]Event) bool { m, _ := a.S.Find(ch, p.TS); return m.Subtype == "tombstone" })
	tl := timelineOf(a, ch)
	if len(tl) != 1 {
		t.Fatalf("timeline %+v", tl)
	}
	v := a.message(ch, &tl[0])
	if !v.Tombstone || v.ReplyCount != 2 {
		t.Errorf("tombstone message %+v", v)
	}
	pushDeleteReply(fs, ch, r2, map[string]any{"type": "message", "subtype": "tombstone", "hidden": true, "ts": p.TS, "thread_ts": p.TS,
		"user": "USLACKBOT", "text": "This message was deleted.", "reply_count": 1, "reply_users": []string{r1.User}, "latest_reply": r1.TS})
	log.wait(t, "tombstone at 1 reply", func([]Event) bool { m, _ := a.S.Find(ch, p.TS); return m.ReplyCount == 1 })
	// last reply: Slack deletes the tombstone itself
	fs.Push(map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "channel": ch, "deleted_ts": r1.TS,
		"previous_message": map[string]any{"type": "message", "ts": r1.TS, "thread_ts": p.TS, "user": r1.User}})
	fs.Push(map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "channel": ch, "deleted_ts": p.TS,
		"previous_message": map[string]any{"type": "message", "subtype": "tombstone", "ts": p.TS, "thread_ts": p.TS, "user": "USLACKBOT"}})
	log.wait(t, "tombstone removed", func([]Event) bool { return len(timelineOf(a, ch)) == 0 })
}

// A thread_broadcast is in the channel and in its thread, once each.
func TestRealtimeBroadcastReply(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fixtureCh // the fake serves no history for it: nothing refetches over the fixture
	p, _, r2 := threadFixture(a, ch)
	ts := fmtTS(time.Now().Unix())
	fs.Push(map[string]any{"type": "message", "subtype": "thread_broadcast", "channel": ch, "ts": ts, "thread_ts": p.TS,
		"user": "U0FAKE00003", "text": "also sent to the channel", "event_ts": ts})
	log.wait(t, "broadcast", func([]Event) bool { _, ok := a.S.Find(ch, ts); return ok })
	if n := countWhere(timelineOf(a, ch), func(m *slack.Message) bool { return m.TS == ts }); n != 1 {
		t.Errorf("broadcast in the channel %d times", n)
	}
	th, _ := a.S.Thread(ch, p.TS)
	if n := countWhere(th, func(m *slack.Message) bool { return m.TS == ts }); n != 1 {
		t.Errorf("broadcast in the thread %d times", n)
	}
	// a plain reply stays in the thread only
	ts2 := fmtTS(time.Now().Unix() + 1)
	fs.Push(map[string]any{"type": "message", "channel": ch, "ts": ts2, "thread_ts": p.TS, "user": r2.User, "text": "thread only", "event_ts": ts2})
	log.wait(t, "reply", func([]Event) bool {
		th, _ := a.S.Thread(ch, p.TS)
		return countWhere(th, func(m *slack.Message) bool { return m.TS == ts2 }) == 1
	})
	if n := countWhere(timelineOf(a, ch), func(m *slack.Message) bool { return m.TS == ts2 }); n != 0 {
		t.Errorf("a plain reply shows in the channel")
	}
}
