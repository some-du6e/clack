package core

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// A followed thread's reply in a conversation core is still looking up (a
// group DM it never saw): the reply's desktop_notification, and a
// dnd_override for the message ahead of it, wait behind the
// conversation's held messages and go out after them in the socket's
// order, as Slack's routeMessages dispatches each event only once
// preprocessRtmMessage has fetched the channel for the messages before
// it. Emitted at once (α.137), the UI showed the reply's toast ahead of
// the message before it and looked the conversation up a second time.
func TestDesktopNotificationWaitsBehindHeldConversation(t *testing.T) {
	a, f := testApp(t)
	evs, stop := a.Subscribe()
	defer stop()
	const ch = "C0GRPHELD1"
	f.mu.Lock()
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"` + ch + `","name":"mpdm-alice--bob--me-9","is_mpim":true}}`
	f.mu.Unlock()
	release, started := gateOnce(f, "conversations.info")
	defer release()
	const root, first, reply = "1762518900.000100", "1762518901.000100", "1762518902.000100"
	a.handle(rtm("message", map[string]any{"channel": ch, "ts": first, "user": "U2", "text": "synthetic"}))
	a.handle(rtm("message", map[string]any{"channel": ch, "ts": reply, "thread_ts": root, "user": "U1", "text": "synthetic"}))
	a.handle(rtm("desktop_notification", map[string]any{"channel": ch, "ts": reply, "msg": reply, "thread_ts": root, "user": "U1", "content": "synthetic"}))
	a.handle(rtm("dnd_override", map[string]any{"channel": ch, "timestamp": first}))
	waitFor(t, "the group DM's conversations.info", 3*time.Second, started)

	// what core announced about ch, in order (ids only)
	var got []string
	take := func(d time.Duration, want int) {
		deadline := time.After(d)
		for len(got) < want {
			select {
			case ev := <-evs:
				if ev.Channel != ch {
					continue
				}
				switch ev.Type {
				case "message":
					got = append(got, "message "+ev.TS)
				case "desktop_notification":
					var e struct {
						Msg string `json:"msg"`
					}
					_ = json.Unmarshal(ev.Raw, &e)
					got = append(got, "desktop_notification "+e.Msg)
				case "dnd_override":
					got = append(got, "dnd_override "+ev.TS)
				}
			case <-deadline:
				return
			}
		}
	}
	take(200*time.Millisecond, 1)
	if len(got) > 0 {
		t.Errorf("core announced %q while the conversation's lookup was out, ahead of its held messages", got)
	}
	release()
	take(5*time.Second, 4)
	a.laterDone(t)
	want := []string{"message " + first, "message " + reply, "desktop_notification " + reply, "dnd_override " + first}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("core announced %q, want the socket's order %q", got, want)
	}
}

// update_thread_state takes the threads' unread state at once, as Slack's
// updateThreadState does: the reply bar's Unread mark (ThreadsUnreadIn)
// and the threads' state move with the event, not with the client.counts
// read 1.5 s later (Karim, 2026-11-22: "i swear i saw your new reply
// before the Unread thing popped up"). A client.counts answer requested
// before the event doesn't put the older state back; an event whose
// org_wide isn't the client's is rejected (THpq: !!org_wide !==
// isEnterprise).
func TestUpdateThreadStateAtOnce(t *testing.T) {
	a, f := testApp(t)
	evs, stop := a.Subscribe()
	defer stop()
	// the backstop read the event also asks for stays off: the test reads
	// the event's state alone
	a.mu.Lock()
	hold := time.AfterFunc(time.Hour, func() {})
	a.countsTimer = hold
	a.mu.Unlock()
	t.Cleanup(func() { hold.Stop() })
	org := a.IsEnterprise()
	f.mu.Lock()
	f.reply["client.counts"] = `{"ok":true,"threads":{"has_unreads":false,"mention_count":0},"channels":[],"ims":[],"mpims":[]}`
	f.mu.Unlock()
	release, started := gateOnce(f, "client.counts")
	defer release()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.refreshCounts(context.Background()) // a read that went out before the reply
	}()
	waitFor(t, "client.counts out", 3*time.Second, started)

	a.handle(rtm("update_thread_state", map[string]any{"has_unreads": true, "mention_count": 0,
		"unread_count_by_channel": map[string]int{"C1": 2}, "mention_count_by_channel": map[string]int{},
		"timestamp": "1762520000.000100", "org_wide": org}))
	if n := a.ThreadsUnreadIn("C1"); n != 2 {
		t.Errorf("right after update_thread_state C1 has %d unread threads, want the event's 2", n)
	}
	if !a.ThreadsState().Unread {
		t.Error("right after update_thread_state the threads read as read")
	}
	next(t, evs, "threads_state")

	a.handle(rtm("update_thread_state", map[string]any{"has_unreads": false, "mention_count": 0,
		"unread_count_by_channel": map[string]int{}, "org_wide": !org}))
	if n := a.ThreadsUnreadIn("C1"); n != 2 || !a.ThreadsState().Unread {
		t.Errorf("an update_thread_state whose org_wide isn't the client's was taken: C1 %d, unread %v", n, a.ThreadsState().Unread)
	}

	release()
	<-done
	if n := a.ThreadsUnreadIn("C1"); n != 2 || !a.ThreadsState().Unread {
		t.Errorf("a client.counts answer requested before the event put its older state back: C1 %d, unread %v", n, a.ThreadsState().Unread)
	}
}
