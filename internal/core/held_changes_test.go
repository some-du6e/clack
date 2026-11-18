package core

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// drainEvents is what evs holds now (emit delivers synchronously).
func drainEvents(evs <-chan Event) []Event {
	var out []Event
	for {
		select {
		case ev, ok := <-evs:
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

// An edit or a deletion of a message that waits for its conversation's
// lookup (a conversation core doesn't know: conversations.info first)
// waits behind it, as Slack's preprocessRtmMessage holds each of the
// conversation's message events until it is fetched. A deletion applied
// at once came before the reply it deleted, whose late "message" put it
// back in the open thread (the hub upserts a thread's message events),
// and an edit came before the copy announced as it was before it.
func TestHeldReplyDeletionWaitsForIt(t *testing.T) {
	a, f := testApp(t)
	evs, stop := a.Subscribe()
	defer stop()
	gate := make(chan struct{})
	f.mu.Lock()
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C6","name":"elsewhere","is_channel":true,"is_member":true}}`
	f.hooks["conversations.info"] = func(url.Values) { <-gate }
	f.mu.Unlock()
	const root, reply, typo = "1762516900.000100", "1762516900.000200", "1762516900.000300"
	a.handle(rtm("message", map[string]any{"channel": "C6", "ts": root, "user": "U2", "text": "a thread"}))
	a.handle(rtm("message", map[string]any{"channel": "C6", "ts": reply, "thread_ts": root, "user": "U2", "text": "a reply"}))
	a.handle(rtm("message", map[string]any{"channel": "C6", "ts": typo, "thread_ts": root, "user": "U2", "text": "a tpyo"}))
	a.handle(rtm("message", map[string]any{"channel": "C6", "subtype": "message_deleted", "deleted_ts": reply, "event_ts": "1762516901.000100",
		"previous_message": map[string]any{"ts": reply, "thread_ts": root, "user": "U2", "text": "a reply"}}))
	a.handle(rtm("message", map[string]any{"channel": "C6", "subtype": "message_changed", "event_ts": "1762516901.000200",
		"message": map[string]any{"ts": typo, "thread_ts": root, "user": "U2", "text": "a typo", "edited": map[string]any{"user": "U2", "ts": "1762516901.000200"}}}))
	close(gate)
	a.laterDone(t)
	got := drainEvents(evs)
	// the open thread as the hub keeps it: a thread's message or update
	// upserts its copy, a delete removes it
	thread := map[string]string{}
	for _, ev := range got {
		switch ev.Type {
		case "message", "update":
			if ev.Msg != nil && ev.Msg.ThreadTS == root && ev.Msg.TS != root {
				thread[ev.Msg.TS] = ev.Msg.Text
			}
		case "delete":
			delete(thread, ev.TS)
		}
	}
	if text, ok := thread[reply]; ok {
		t.Errorf("the deleted reply %q is back in the open thread; events: %s", text, evTypes(got))
	}
	if text := thread[typo]; text != "a typo" {
		t.Errorf("the edited reply reads %q in the open thread, want %q; events: %s", text, "a typo", evTypes(got))
	}
	if _, ok := a.S.Find("C6", reply); ok {
		t.Error("the deleted reply is still stored")
	}
}

// A message event of a history_changed subtype is no new message: Slack's
// messageReceived returns for it, and only the event of that type
// (channel_history_changed and its kin) purges anything. Taken for a new
// message, it was stored and announced as one.
func TestHistoryChangedSubtypeIsNoMessage(t *testing.T) {
	a, _ := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{TS: "1762516800.000100", User: "U1", Text: "before"}}, false) // a window the message would join
	evs, stop := a.Subscribe()
	defer stop()
	for i, sub := range []string{"channel_history_changed", "group_history_changed", "im_history_changed", "mpim_history_changed"} {
		ts := fmt.Sprintf("1762516900.%06d", 100+i)
		a.handleNow(t, rtm("message", map[string]any{"channel": "C1", "subtype": sub, "ts": ts, "event_ts": ts, "latest": ts}))
		if _, ok := a.S.Find("C1", ts); ok {
			t.Errorf("%s: stored as a message", sub)
		}
	}
	for _, ev := range drainEvents(evs) {
		if ev.Type == "message" {
			t.Errorf("a history_changed message was announced: %s %+v", ev.TS, ev.Msg)
		}
	}
}
