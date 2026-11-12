package core

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Pins arriving in any order against the message they pin (Karim, α.43,
// 2026-11-12: a message posted and pinned within the same second showed
// no pin). Slack's pinAdded upserts the pin into its messages store
// whether or not the message is loaded, and a later socket copy merges
// into it (store/pins.go); every path here ends pinned (or not) as
// Slack's own copy is.

const pinAuthor = "U0FAKE00001"

// pinnedIn reports the cached copy's pin state and pinner.
func pinnedIn(a *App, ch, ts string) (pinned bool, by string, found bool) {
	m, ok := a.S.Find(ch, ts)
	if !ok {
		return false, "", false
	}
	v := a.message(ch, &m)
	return v.Pinned, v.PinnedBy, true
}

// waitPinned waits for the cached copy of ts to reach want.
func waitPinned(t *testing.T, a *App, ch, ts string, want bool) {
	t.Helper()
	dl := time.Now().Add(5 * time.Second)
	for {
		p, by, ok := pinnedIn(a, ch, ts)
		if ok && p == want && (!want || by == fakeslack.SelfID) {
			return
		}
		if time.Now().After(dl) {
			t.Fatalf("%s/%s: cached %v, pinned %v by %q; want pinned %v by you", ch, ts, ok, p, by, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// messageEvent is the "message" event core emitted for ts, once it came.
func messageEvent(t *testing.T, log *eventLog, ts string) Event {
	t.Helper()
	var got Event
	log.wait(t, "message "+ts, func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "message" && ev.TS == ts && ev.Msg != nil {
				got = ev
				return true
			}
		}
		return false
	})
	return got
}

func mustPin(t *testing.T, fs *fakeslack.Server, ch, ts string, on bool) map[string]any {
	t.Helper()
	ev, code := fs.Pin(fakeslack.SelfID, ch, ts, on)
	if code != "" {
		t.Fatalf("Pin(%s, %v): %s", ts, on, code)
	}
	return ev
}

// The pin event before the message event: the message arrives pinned,
// in the store and in the event the UI gets.
func TestPinBeforeMessage(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fs.ChannelIDs()[1]
	if _, err := a.History(context.Background(), ch, ""); err != nil {
		t.Fatal(err)
	}
	ts, msg := fs.PostQuiet(ch, pinAuthor, "anchored synthetic")
	fs.Push(mustPin(t, fs, ch, ts, true))
	fs.Push(msg)
	if ev := messageEvent(t, log, ts); !ev.Msg.Pinned || ev.Msg.PinnedBy != fakeslack.SelfID {
		t.Errorf("message event: pinned %v by %q, want by you", ev.Msg.Pinned, ev.Msg.PinnedBy)
	}
	waitPinned(t, a, ch, ts, true)
}

// The pin event after the message: an update for it, pinned.
func TestPinAfterMessage(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fs.ChannelIDs()[1]
	if _, err := a.History(context.Background(), ch, ""); err != nil {
		t.Fatal(err)
	}
	ts, msg := fs.PostQuiet(ch, pinAuthor, "anchored synthetic")
	fs.Push(msg)
	messageEvent(t, log, ts)
	fs.Push(mustPin(t, fs, ch, ts, true))
	log.wait(t, "pinned update", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "update" && ev.TS == ts && ev.Msg != nil && ev.Msg.Pinned {
				return true
			}
		}
		return false
	})
	waitPinned(t, a, ch, ts, true)
}

// Pinned and unpinned before the message came: it arrives unpinned, and
// stays so through a refetch.
func TestUnpinBeforeMessage(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fs.ChannelIDs()[1]
	if _, err := a.History(context.Background(), ch, ""); err != nil {
		t.Fatal(err)
	}
	ts, msg := fs.PostQuiet(ch, pinAuthor, "anchored synthetic")
	fs.Push(mustPin(t, fs, ch, ts, true))
	fs.Push(mustPin(t, fs, ch, ts, false))
	fs.Push(msg)
	if ev := messageEvent(t, log, ts); ev.Msg.Pinned {
		t.Error("message event pinned after pin_removed")
	}
	waitPinned(t, a, ch, ts, false)
	if _, err := a.Refresh(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	waitPinned(t, a, ch, ts, false)
}

// Pinned in a conversation nothing loaded yet: opening it shows the pin
// (Slack's history carries it), also when the pin event lands while that
// history read is out and its answer predates the pin.
func TestPinUnloadedChannelThenOpen(t *testing.T) {
	t.Run("pinned before open", func(t *testing.T) {
		a, fs, _ := rtApp(t)
		ch := fs.ChannelIDs()[2]
		if a.S.Timeline(ch) != nil {
			t.Log("prefetch loaded the channel already") // the next subtest forces the read
		}
		ts, msg := fs.PostQuiet(ch, pinAuthor, "anchored synthetic")
		fs.Push(msg)
		fs.Push(mustPin(t, fs, ch, ts, true))
		dl := time.Now().Add(5 * time.Second)
		for a.S.PinSeq() == 0 && time.Now().Before(dl) {
			time.Sleep(5 * time.Millisecond)
		}
		if _, err := a.History(context.Background(), ch, ""); err != nil {
			t.Fatal(err)
		}
		waitPinned(t, a, ch, ts, true)
	})
	t.Run("pin during the history read", func(t *testing.T) {
		a, fs, _ := rtApp(t)
		ch := fs.ChannelIDs()[3]
		ts, msg := fs.PostQuiet(ch, pinAuthor, "anchored synthetic")
		fs.Push(msg)
		before := fs.CallCount("conversations.history")
		fs.SetFault("conversations.history", fakeslack.Fault{ReplyDelay: 600 * time.Millisecond, Times: 1})
		done := make(chan error, 1)
		go func() { done <- a.loadLatest(context.Background(), ch) }()
		dl := time.Now().Add(5 * time.Second)
		for fs.CallCount("conversations.history") == before && time.Now().Before(dl) {
			time.Sleep(2 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond) // the answer is made (unpinned) and held
		seq := a.S.PinSeq()
		fs.Push(mustPin(t, fs, ch, ts, true))
		for a.S.PinSeq() == seq && time.Now().Before(dl) {
			time.Sleep(2 * time.Millisecond)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		waitPinned(t, a, ch, ts, true)
	})
}

// A thread reply pinned before it arrives keeps the pin in the open
// thread.
func TestPinThreadReplyBeforeReply(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fs.ChannelIDs()[1]
	if _, err := a.History(context.Background(), ch, ""); err != nil {
		t.Fatal(err)
	}
	tl := timelineOf(a, ch)
	parent := tl[len(tl)-1].TS
	a.S.SetThread(ch, parent, []slack.Message{{Type: "message", TS: parent, ThreadTS: parent, User: pinAuthor, Text: "root"}})
	now := time.Now()
	reply := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
	item := map[string]any{"type": "message", "ts": reply, "thread_ts": parent, "user": pinAuthor, "text": "pinned reply"}
	fs.Push(map[string]any{"type": "pin_added", "user": fakeslack.SelfID, "channel_id": ch, "event_ts": reply, "pin_count": 1,
		"pinned_info": map[string]any{"channel": ch, "pinned_by": fakeslack.SelfID, "pinned_ts": now.Unix()},
		"item":        map[string]any{"type": "message", "channel": ch, "created": now.Unix(), "created_by": fakeslack.SelfID, "message": item}})
	fs.Push(map[string]any{"type": "message", "channel": ch, "user": pinAuthor, "text": "pinned reply", "ts": reply,
		"thread_ts": parent, "event_ts": reply})
	if ev := messageEvent(t, log, reply); !ev.Msg.Pinned {
		t.Error("reply's message event isn't pinned")
	}
	th, _ := a.S.Thread(ch, parent)
	got := false
	for i := range th {
		if th[i].TS == reply {
			got = a.message(ch, &th[i]).Pinned
		}
	}
	if !got {
		t.Errorf("thread copy of the reply isn't pinned: %+v", th)
	}
}

// The viewer's own send, pinned right away: whichever of the reply, the
// pin and the echo comes first, it ends pinned (a late echo used to
// replace the pinned row with an unpinned copy).
func TestPinOwnSend(t *testing.T) {
	for _, tc := range []struct {
		name        string
		pinPrereply bool // pin_added before chat.postMessage answers
	}{
		{"pin after the reply, echo last", false},
		{"pin before the reply, echo last", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, fs, log := rtApp(t)
			ch := fs.ChannelIDs()[1]
			if _, err := a.History(context.Background(), ch, ""); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var posted map[string]any
			pin := func(msg map[string]any) {
				ts, _ := msg["ts"].(string)
				fs.Push(map[string]any{"type": "pin_added", "user": fakeslack.SelfID, "channel_id": ch, "event_ts": ts, "pin_count": 1,
					"pinned_info": map[string]any{"channel": ch, "pinned_by": fakeslack.SelfID, "pinned_ts": time.Now().Unix()},
					"item":        map[string]any{"type": "message", "channel": ch, "created_by": fakeslack.SelfID, "message": msg}})
			}
			seq := a.S.PinSeq()
			fs.OnPost = func(_ url.Values, msg map[string]any) {
				mu.Lock()
				posted = msg
				mu.Unlock()
				if tc.pinPrereply {
					pin(msg)
					dl := time.Now().Add(5 * time.Second)
					for a.S.PinSeq() == seq && time.Now().Before(dl) { // applied before the reply
						time.Sleep(2 * time.Millisecond)
					}
				}
			}
			got, err := a.Send(context.Background(), SendOpts{Channel: ch, Text: "anchored own"})
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			msg := posted
			mu.Unlock()
			if !tc.pinPrereply {
				pin(msg)
				waitPinned(t, a, ch, got.TS, true)
			}
			if tc.pinPrereply && !got.Pinned {
				t.Error("Send's message isn't pinned")
			}
			ev := map[string]any{"type": "message", "channel": ch, "event_ts": msg["ts"]}
			for k, v := range msg {
				ev[k] = v
			}
			n := len(log.all())
			fs.Push(ev) // the late echo
			var echo Event
			log.wait(t, "echo applied", func(evs []Event) bool {
				for _, e := range evs[n:] {
					if e.Type == "update" && e.TS == got.TS && e.Msg != nil {
						echo = e
						return true
					}
				}
				return false
			})
			if !echo.Msg.Pinned || echo.Msg.PinnedBy != fakeslack.SelfID {
				t.Errorf("after the echo: pinned %v by %q, want by you", echo.Msg.Pinned, echo.Msg.PinnedBy)
			}
			waitPinned(t, a, ch, got.TS, true)
		})
	}
}

// With realtime off the 20 s poll refetches the open conversation, whose
// page carries pinned_to: a pin made elsewhere shows, and so does its
// removal.
func TestPinThroughPolling(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	a.pollEvery = 30 * time.Millisecond
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	ch := fs.ChannelIDs()[1]
	if _, err := a.History(context.Background(), ch, ""); err != nil {
		t.Fatal(err)
	}
	tl := timelineOf(a, ch)
	ts := tl[len(tl)-1].TS
	mustPin(t, fs, ch, ts, true) // nobody hears the event
	waitPinned(t, a, ch, ts, true)
	mustPin(t, fs, ch, ts, false)
	waitPinned(t, a, ch, ts, false)
}
