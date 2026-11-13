package core

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// A thread parent's reply_count comes from the server (message_replied, a
// broadcast's root, the parent's message_changed), never from counting
// replies locally (thread_meta.go). Karim's thread showed "2 replies" for
// one: message_replied came first and the reply's own event bumped it again.

// fixtureFlushCh is a channel the fixture doesn't use: a message there,
// pushed after the events under test, tells when they've been applied
// (the socket's events are handled in order).
const fixtureFlushCh = "C0TESTFIX02"

var flushN atomic.Int64

func flush(t *testing.T, fs *fakeslack.Server, log *eventLog) {
	t.Helper()
	ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), 900000+flushN.Add(1))
	fs.Push(map[string]any{"type": "message", "channel": fixtureFlushCh, "ts": ts, "user": "U0FAKE00003", "text": "flush", "event_ts": ts})
	log.wait(t, "flush", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "message" && ev.TS == ts {
				return true
			}
		}
		return false
	})
}

// replyEvent is a reply's message event; replied the parent's
// message_replied with n replies, the latest at latest.
func replyEvent(ch string, r slack.Message) map[string]any {
	return map[string]any{"type": "message", "channel": ch, "ts": r.TS, "thread_ts": r.ThreadTS, "user": r.User, "text": r.Text, "event_ts": r.TS}
}

func repliedEvent(ch string, p slack.Message, n int, users []string, latest string) map[string]any {
	return map[string]any{"type": "message", "subtype": "message_replied", "hidden": true, "channel": ch, "event_ts": latest, "ts": latest,
		"message": map[string]any{"type": "message", "ts": p.TS, "thread_ts": p.TS, "user": p.User, "text": p.Text,
			"reply_count": n, "reply_users": users, "reply_users_count": len(users), "latest_reply": latest}}
}

func parentCount(t *testing.T, a *App, ch, ts string) slack.Message {
	t.Helper()
	m, ok := a.S.Find(ch, ts)
	if !ok {
		t.Fatalf("parent %s not cached", ts)
	}
	return m
}

func threadCopies(a *App, ch, parent, ts string) int {
	th, _ := a.S.Thread(ch, parent)
	return countWhere(th, func(m *slack.Message) bool { return m.TS == ts })
}

// Both orders of a reply and its message_replied, and both delivered twice.
func TestReplyCountEventOrders(t *testing.T) {
	for _, tc := range []struct {
		name         string
		repliedFirst bool
		twice        bool
	}{
		{"reply then message_replied", false, false},
		{"message_replied then reply", true, false}, // Karim's thread, 2026-11-12
		{"duplicate delivery, reply first", false, true},
		{"duplicate delivery, message_replied first", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, fs, log := rtApp(t)
			ch := fixtureCh
			p, _, _ := threadFixture(a, ch)
			r3 := slack.Message{TS: fmtTS(time.Now().Unix() - 30), ThreadTS: p.TS, User: "U0FAKE00003", Text: "three"}
			users := []string{"U0FAKE00002", "U0FAKE00003"}
			evs := []map[string]any{replyEvent(ch, r3), repliedEvent(ch, p, 3, users, r3.TS)}
			if tc.repliedFirst {
				evs[0], evs[1] = evs[1], evs[0]
			}
			if tc.twice {
				evs = append(evs, evs...)
			}
			for _, ev := range evs {
				fs.Push(ev)
			}
			flush(t, fs, log)
			m := parentCount(t, a, ch, p.TS)
			if m.ReplyCount != 3 || m.LatestReply != r3.TS || len(m.ReplyUsers) != 2 {
				t.Errorf("parent reply_count %d latest %s users %v; want 3, %s, 2 users", m.ReplyCount, m.LatestReply, m.ReplyUsers, r3.TS)
			}
			if n := threadCopies(a, ch, p.TS, r3.TS); n != 1 {
				t.Errorf("reply cached %d times in its thread", n)
			}
			if v := a.message(ch, &timelineOf(a, ch)[0]); v.ReplyCount != 3 {
				t.Errorf("UI parent reply count %d", v.ReplyCount)
			}
		})
	}
}

// messageReplied drops an event older than the stored parent's
// latest_reply; a reply alone (message_replied lost) doesn't count.
func TestReplyCountStaleReplied(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fixtureCh
	p, r1, r2 := threadFixture(a, ch)
	fs.Push(repliedEvent(ch, p, 1, []string{r1.User}, r1.TS)) // older than the cached latest_reply (r2)
	r3 := slack.Message{TS: fmtTS(time.Now().Unix() - 30), ThreadTS: p.TS, User: "U0FAKE00003", Text: "three"}
	fs.Push(replyEvent(ch, r3))
	flush(t, fs, log)
	if m := parentCount(t, a, ch, p.TS); m.ReplyCount != 2 || m.LatestReply != r2.TS {
		t.Errorf("parent %d / %s, want the cached 2 / %s", m.ReplyCount, m.LatestReply, r2.TS)
	}
	if staleReplied("", &p) || !staleReplied(r1.TS, &p) || staleReplied(r2.TS, &p) || staleReplied(r1.TS, &slack.Message{LatestReply: r2.TS}) {
		t.Error("staleReplied disagrees with messageReplied's guard")
	}
}

// The viewer's own reply: the echo, the chat.postMessage reply and
// message_replied in every order give one reply and the server's count.
func TestReplyCountOwnSend(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order string // e: echo, r: message_replied, both pushed before the API reply unless after
		after bool
	}{
		{"echo, replied, then API reply", "er", false},
		{"replied, echo, then API reply", "re", false},
		{"API reply, then echo, replied", "er", true},
		{"API reply, then replied, echo", "re", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, fs, log := rtApp(t)
			ch := fixtureCh
			p, _, _ := threadFixture(a, ch)
			var mu sync.Mutex
			var events []map[string]any
			fs.OnPost = func(_ url.Values, msg map[string]any) {
				echo := map[string]any{"type": "message", "channel": ch, "event_ts": msg["ts"]}
				for k, v := range msg {
					echo[k] = v
				}
				ts, _ := msg["ts"].(string)
				replied := repliedEvent(ch, p, 3, []string{"U0FAKE00002", fakeslack.SelfID}, ts)
				evs := []map[string]any{echo, replied}
				if tc.order == "re" {
					evs = []map[string]any{replied, echo}
				}
				if !tc.after {
					for _, ev := range evs {
						fs.Push(ev)
					}
					time.Sleep(50 * time.Millisecond) // let them land before the reply
					return
				}
				mu.Lock()
				events = evs
				mu.Unlock()
			}
			got, err := a.Send(context.Background(), SendOpts{Channel: ch, ThreadTS: p.TS, Text: "mine"})
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			for _, ev := range events {
				fs.Push(ev)
			}
			mu.Unlock()
			log.wait(t, "message_replied applied", func([]Event) bool { return parentCount(t, a, ch, p.TS).LatestReply == got.TS })
			flush(t, fs, log)
			if m := parentCount(t, a, ch, p.TS); m.ReplyCount != 3 {
				t.Errorf("parent reply_count %d, want 3", m.ReplyCount)
			}
			if n := threadCopies(a, ch, p.TS, got.TS); n != 1 {
				t.Errorf("own reply cached %d times", n)
			}
		})
	}
}

// A broadcast's root carries the parent's metadata (makeBroadcastRootObject);
// a root without it leaves the parent alone, and message_replied after
// either agrees.
func TestReplyCountBroadcast(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fixtureCh
	p, _, _ := threadFixture(a, ch)
	ts := fmtTS(time.Now().Unix() - 30)
	users := []string{"U0FAKE00002", "U0FAKE00003"}
	fs.Push(map[string]any{"type": "message", "subtype": "thread_broadcast", "channel": ch, "ts": ts, "thread_ts": p.TS, "event_ts": ts,
		"user": "U0FAKE00003", "text": "also sent to the channel",
		"root": map[string]any{"type": "message", "ts": p.TS, "thread_ts": p.TS, "user": p.User, "text": p.Text,
			"reply_count": 3, "reply_users": users, "reply_users_count": 2, "latest_reply": ts}})
	flush(t, fs, log)
	if m := parentCount(t, a, ch, p.TS); m.ReplyCount != 3 || m.LatestReply != ts || len(m.ReplyUsers) != 2 {
		t.Errorf("parent after the broadcast %d / %s / %v", m.ReplyCount, m.LatestReply, m.ReplyUsers)
	}
	fs.Push(repliedEvent(ch, p, 3, users, ts))
	flush(t, fs, log)
	if m := parentCount(t, a, ch, p.TS); m.ReplyCount != 3 {
		t.Errorf("parent after message_replied %d, want 3", m.ReplyCount)
	}
	// a root with no thread fields: nothing to take
	ts2 := fmtTS(time.Now().Unix() - 20)
	fs.Push(map[string]any{"type": "message", "subtype": "thread_broadcast", "channel": ch, "ts": ts2, "thread_ts": p.TS, "event_ts": ts2,
		"user": "U0FAKE00003", "text": "again", "root": map[string]any{"type": "message", "ts": p.TS, "user": p.User, "text": p.Text}})
	flush(t, fs, log)
	if m := parentCount(t, a, ch, p.TS); m.ReplyCount != 3 || m.LatestReply != ts {
		t.Errorf("a bare root changed the parent: %d / %s", m.ReplyCount, m.LatestReply)
	}
	if n := countWhere(timelineOf(a, ch), func(m *slack.Message) bool { return m.TS == ts || m.TS == ts2 }); n != 2 {
		t.Errorf("%d broadcasts in the channel, want 2", n)
	}
}

// A reply deleted afterwards: message_deleted only removes it
// (setMessageDeleted); the parent's message_changed brings the count down.
func TestReplyCountDeletedReply(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fixtureCh
	p, _, _ := threadFixture(a, ch)
	r3 := slack.Message{TS: fmtTS(time.Now().Unix() - 30), ThreadTS: p.TS, User: "U0FAKE00003", Text: "three"}
	users := []string{"U0FAKE00002", "U0FAKE00003"}
	fs.Push(repliedEvent(ch, p, 3, users, r3.TS))
	fs.Push(replyEvent(ch, r3))
	flush(t, fs, log)
	if m := parentCount(t, a, ch, p.TS); m.ReplyCount != 3 {
		t.Fatalf("parent %d before the delete, want 3", m.ReplyCount)
	}
	fs.Push(map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "channel": ch, "deleted_ts": r3.TS, "event_ts": r3.TS,
		"previous_message": map[string]any{"type": "message", "ts": r3.TS, "thread_ts": p.TS, "user": r3.User, "text": r3.Text}})
	flush(t, fs, log)
	if n := threadCopies(a, ch, p.TS, r3.TS); n != 0 {
		t.Errorf("deleted reply still in the thread")
	}
	if m := parentCount(t, a, ch, p.TS); m.ReplyCount != 3 {
		t.Errorf("message_deleted alone moved the parent to %d; Slack waits for its message_changed", m.ReplyCount)
	}
	fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch, "event_ts": r3.TS,
		"message": map[string]any{"type": "message", "ts": p.TS, "thread_ts": p.TS, "user": p.User, "text": p.Text,
			"reply_count": 2, "reply_users": []string{"U0FAKE00002"}, "reply_users_count": 1, "latest_reply": fmtTS(time.Now().Unix() - 58)}})
	flush(t, fs, log)
	if m := parentCount(t, a, ch, p.TS); m.ReplyCount != 2 {
		t.Errorf("parent after its message_changed %d, want 2", m.ReplyCount)
	}
}
