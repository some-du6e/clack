package core

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// laterDone waits until the event pump's later work queued so far ran
// (pump_later.go): the lookups a handled event left, and what they
// announce.
func (a *App) laterDone(t testing.TB) {
	t.Helper()
	busy := func(q *laterQueue) bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		return q.running || len(q.tasks) > 0
	}
	// the pump's queue, and the conversations' held events (convLater)
	idle := func() bool {
		if busy(&a.pumpQ) {
			return false
		}
		a.convHoldMu.Lock()
		defer a.convHoldMu.Unlock()
		if len(a.convHold) > 0 {
			return false
		}
		for _, q := range a.convQs {
			if busy(q) {
				return false
			}
		}
		return true
	}
	for dl := time.Now().Add(15 * time.Second); ; time.Sleep(time.Millisecond) {
		if idle() {
			return
		}
		if time.Now().After(dl) {
			t.Fatal("the event pump's later work never finished")
		}
	}
}

// handleNow is handle and the lookups it left (laterDone).
func (a *App) handleNow(t testing.TB, ev slack.Event) {
	t.Helper()
	a.handle(ev)
	a.laterDone(t)
}

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

// awaitMessage reads evs until the message event of ch/ts (true), or d
// passes (false); the events read on the way go to seen.
func awaitMessage(evs <-chan Event, ch, ts string, d time.Duration, seen *[]Event) bool {
	deadline := time.After(d)
	for {
		select {
		case ev := <-evs:
			*seen = append(*seen, ev)
			if ev.Type == "message" && ev.Channel == ch && ev.TS == ts {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// A socket event's lookup runs off the event pump: the message right
// behind it is announced while the lookup is still out, and the lookup's
// answer lands after. On the pump, every message behind users.prefs after
// a pref_change (Clack's own frecency saves bring one about once a
// minute), the sections reread, a self-join's conversations.info and the
// others waited for their round trip, and reached the lists late (Karim:
// "messages show late"). Slack's socket handler takes the next event
// while a lookup is out.
func TestSocketLookupsLeaveThePump(t *testing.T) {
	const savedTS = "1762516950.000100"
	cases := []struct {
		name   string
		method string // the lookup (the fake's last path segment)
		reply  string
		setup  func(a *App)
		ev     slack.Event
		landed func(a *App, f *fakeSlack, got []Event) bool
	}{
		{name: "pref_change's users.prefs", method: "users.prefs.get",
			reply: `{"ok":true,"prefs":{"synthetic_fetched":"yes"}}`,
			ev:    rtm("pref_change", map[string]any{"name": "display_real_names", "value": true}),
			landed: func(a *App, _ *fakeSlack, _ []Event) bool {
				a.S.RLock()
				defer a.S.RUnlock()
				return string(a.S.RawPrefs["synthetic_fetched"]) == `"yes"`
			}},
		{name: "the sections reread", method: "users.channelSections.list",
			reply: `{"ok":true,"channel_sections":[{"channel_section_id":"S9","name":"Synthetic","type":"standard","channel_ids_page":{"channel_ids":["C1"]}}]}`,
			ev:    rtm("channel_sections_upserted", map[string]any{"channel_section_id": "S9"}),
			landed: func(a *App, _ *fakeSlack, _ []Event) bool {
				a.S.RLock()
				defer a.S.RUnlock()
				for _, s := range a.S.Sections {
					if s.ID == "S9" {
						return true
					}
				}
				return false
			}},
		{name: "a self-join's conversations.info", method: "conversations.info",
			reply: `{"ok":true,"channel":{"id":"C7","name":"joined-now","is_channel":true,"is_member":true}}`,
			ev:    rtm("member_joined_channel", map[string]any{"channel": "C7", "user": "USELF"}),
			landed: func(a *App, _ *fakeSlack, _ []Event) bool {
				_, ok := a.S.Conv("C7")
				return ok
			}},
		{name: "channel_joined's conversations.info", method: "conversations.info",
			reply: `{"ok":true,"channel":{"id":"C8","name":"c8","is_channel":true,"is_member":true}}`,
			ev:    rtm("channel_joined", map[string]any{"channel": map[string]any{"id": "C8", "name": "c8"}}),
			landed: func(a *App, _ *fakeSlack, _ []Event) bool {
				_, ok := a.S.Conv("C8")
				return ok
			}},
		{name: "a group DM's conversion to a channel", method: "conversations.info",
			reply: `{"ok":true,"channel":{"id":"C9","name":"was-a-group-dm","is_channel":true,"is_private":true,"is_member":true}}`,
			ev:    rtm("mpim_close", map[string]any{"channel": "G1", "converted_to": "C9"}),
			landed: func(a *App, _ *fakeSlack, _ []Event) bool {
				_, ok := a.S.Conv("C9")
				return ok
			}},
		{name: "dnd_updated's dnd.info", method: "dnd.info",
			reply: `{"ok":true,"dnd_enabled":true,"next_dnd_start_ts":1762600000,"next_dnd_end_ts":1762603600}`,
			ev:    rtm("dnd_updated", map[string]any{"user": "USELF", "dnd_status": map[string]any{"dnd_enabled": true, "next_dnd_start_ts": 1, "next_dnd_end_ts": 2}}),
			landed: func(a *App, _ *fakeSlack, _ []Event) bool {
				a.dnd.mu.Lock()
				defer a.dnd.mu.Unlock()
				return a.dnd.status.NextStart == 1762600000
			}},
		{name: "a saved message's messages.list", method: "messages.list",
			reply: `{"ok":true,"messages":{"C1":[{"type":"message","ts":"` + savedTS + `","user":"U1","text":"a synthetic saved message"}]}}`,
			ev:    rtm("saved_added", map[string]any{"saved": map[string]any{"item_id": "C1", "item_type": "message", "ts": savedTS, "date_created": 1762516960, "state": "in_progress"}}),
			landed: func(_ *App, _ *fakeSlack, got []Event) bool {
				for _, ev := range got {
					if ev.Type == "saved" && ev.TS == savedTS && ev.Saved != nil && ev.Saved.Msg != nil {
						return true
					}
				}
				return false
			}},
		{name: "search_frecency's users.prefs", method: "users.prefs.get",
			reply: `{"ok":true,"prefs":{"emoji_use":"{}","frecency":"{}"}}`,
			ev:    slack.Event{Type: "search_frecency", Raw: json.RawMessage(`{"type":"search_frecency","value":"updated"}`)},
			landed: func(_ *App, f *fakeSlack, _ []Event) bool {
				return strings.Contains(f.last("users.prefs.get").Get("prefs"), prefFrecency)
			}},
		{name: "search_recents's users.prefs", method: "users.prefs.get",
			reply: `{"ok":true,"prefs":{"recents":"[]"}}`,
			setup: func(a *App) { a.setExperiments(map[string]string{"desktop_search_recents_read": "on"}) },
			ev:    slack.Event{Type: "search_recents", Raw: json.RawMessage(`{"type":"search_recents","value":"updated"}`)},
			landed: func(_ *App, f *fakeSlack, _ []Event) bool {
				return f.last("users.prefs.get").Get("prefs") == prefRecents
			}},
		{name: "an assistant status's unknown bot", method: "info", // edge users/info
			reply: `{"ok":true,"results":[{"id":"UNEWBOT","name":"newbot","is_bot":true,"real_name":"New Bot","profile":{"real_name":"New Bot","display_name":"newbot"}}],"failed_ids":[]}`,
			ev: slack.Event{Type: "ai_assistant_status", Raw: raw(map[string]any{"type": "ai_assistant_status", "channel_id": "D1", "thread_ts": "1.0",
				"bot_user_id": "UNEWBOT", "status": "is thinking…"})},
			landed: func(_ *App, _ *fakeSlack, got []Event) bool {
				for _, ev := range got {
					if ev.Type == "assistant_status" && ev.UserID == "UNEWBOT" && ev.Name != "" {
						return true
					}
				}
				return false
			}},
		{name: "a message of a conversation core doesn't know", method: "conversations.info",
			reply: `{"ok":true,"channel":{"id":"C6","name":"elsewhere","is_channel":true,"is_member":true}}`,
			ev:    rtm("message", map[string]any{"channel": "C6", "ts": "1762516990.000100", "user": "U2", "text": "in a channel core doesn't know"}),
			landed: func(a *App, _ *fakeSlack, got []Event) bool {
				for _, ev := range got {
					if ev.Type == "message" && ev.Channel == "C6" {
						_, ok := a.S.Conv("C6")
						return ok
					}
				}
				return false
			}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, f := testApp(t)
			if tc.setup != nil {
				tc.setup(a)
			}
			evs, stop := a.Subscribe()
			defer stop()
			gate := make(chan struct{})
			released := false
			release := func() {
				if !released {
					released = true
					close(gate)
				}
			}
			defer release()
			f.mu.Lock()
			f.reply[tc.method] = tc.reply
			f.hooks[tc.method] = func(url.Values) { <-gate }
			f.mu.Unlock()
			ts := fmt.Sprintf("1762517%03d.000100", i)
			done := make(chan struct{})
			go func() { // as the pump takes them: one after another
				defer close(done)
				a.handle(tc.ev)
				a.handle(rtm("message", map[string]any{"channel": "C1", "ts": ts, "user": "U1", "text": "right behind it"}))
			}()
			var got []Event
			if !awaitMessage(evs, "C1", ts, 2*time.Second, &got) {
				t.Errorf("the message right behind %s waited for its %s", tc.name, tc.method)
			}
			release()
			<-done
			a.laterDone(t)
			got = append(got, drainEvents(evs)...)
			if !tc.landed(a, f, got) {
				t.Errorf("%s: what %s answered never landed; events: %s", tc.name, tc.method, evTypes(got))
			}
		})
	}
}

// A pref_change's value applies at once, as Slack's prefChange sets it:
// the messages right behind the event notify and draw with it, not with
// the prefs from before users.prefs answers (read off the pump).
func TestPrefChangeValueAppliesAtOnce(t *testing.T) {
	a, f := testApp(t)
	evs, stop := a.Subscribe()
	defer stop()
	gate := make(chan struct{})
	f.mu.Lock()
	f.reply["users.prefs.get"] = `{"ok":true,"prefs":{"display_real_names":true,"synthetic_fetched":"yes"}}`
	f.hooks["users.prefs.get"] = func(url.Values) { <-gate }
	f.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.handle(rtm("pref_change", map[string]any{"name": "display_real_names", "value": true}))
	}()
	v := ""
	for dl := time.Now().Add(2 * time.Second); v != "true" && time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
		a.S.RLock()
		v = string(a.S.RawPrefs["display_real_names"])
		a.S.RUnlock()
	}
	if v != "true" {
		t.Errorf("display_real_names reads %q while users.prefs is out, want true (the event's value)", v)
	}
	sawPrefs := false
	for _, ev := range drainEvents(evs) {
		sawPrefs = sawPrefs || ev.Type == "prefs"
	}
	if !sawPrefs {
		t.Error("no prefs event before users.prefs answered")
	}
	close(gate)
	<-done
	a.laterDone(t)
	a.S.RLock()
	fetched := string(a.S.RawPrefs["synthetic_fetched"])
	a.S.RUnlock()
	if fetched != `"yes"` {
		t.Errorf("users.prefs' map never installed (synthetic_fetched %q)", fetched)
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.handle(rtm("message", map[string]any{"channel": "C6", "ts": root, "user": "U2", "text": "a thread"}))
		a.handle(rtm("message", map[string]any{"channel": "C6", "ts": reply, "thread_ts": root, "user": "U2", "text": "a reply"}))
		a.handle(rtm("message", map[string]any{"channel": "C6", "ts": typo, "thread_ts": root, "user": "U2", "text": "a tpyo"}))
		a.handle(rtm("message", map[string]any{"channel": "C6", "subtype": "message_deleted", "deleted_ts": reply, "event_ts": "1762516901.000100",
			"previous_message": map[string]any{"ts": reply, "thread_ts": root, "user": "U2", "text": "a reply"}}))
		a.handle(rtm("message", map[string]any{"channel": "C6", "subtype": "message_changed", "event_ts": "1762516901.000200",
			"message": map[string]any{"ts": typo, "thread_ts": root, "user": "U2", "text": "a typo", "edited": map[string]any{"user": "U2", "ts": "1762516901.000200"}}}))
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("the pump waited for the conversation's lookup")
	}
	close(gate)
	<-done
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

// A conversation's held messages go out in the order they came, after
// its lookup, and a message that comes once the lookup answered goes out
// at once behind them.
func TestHeldMessagesKeepTheirOrder(t *testing.T) {
	a, f := testApp(t)
	evs, stop := a.Subscribe()
	defer stop()
	gate := make(chan struct{})
	f.mu.Lock()
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C6","name":"elsewhere","is_channel":true,"is_member":true}}`
	f.hooks["conversations.info"] = func(url.Values) { <-gate }
	f.mu.Unlock()
	var want []string
	for i := 0; i < 4; i++ {
		want = append(want, fmt.Sprintf("1762516910.%06d", 100+i))
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i, ts := range want {
			a.handle(rtm("message", map[string]any{"channel": "C6", "ts": ts, "user": "U2", "text": fmt.Sprintf("held %d", i)}))
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("the pump waited for C6's lookup")
	}
	var early []Event
	if n := countMessages(append(early, drainEvents(evs)...), "C6"); n != 0 {
		t.Errorf("%d of C6's messages went out before its lookup answered", n)
	}
	close(gate)
	<-done
	a.laterDone(t)
	const after = "1762516911.000100"
	want = append(want, after)
	a.handleNow(t, rtm("message", map[string]any{"channel": "C6", "ts": after, "user": "U2", "text": "after the lookup"}))
	var order []string
	for _, ev := range drainEvents(evs) {
		if ev.Type == "message" && ev.Channel == "C6" {
			order = append(order, ev.TS)
		}
	}
	if strings.Join(order, " ") != strings.Join(want, " ") {
		t.Errorf("C6's messages went out as %v, want %v", order, want)
	}
}

func countMessages(evs []Event, ch string) int {
	n := 0
	for _, ev := range evs {
		if ev.Type == "message" && ev.Channel == ch {
			n++
		}
	}
	return n
}

// A message held for its conversation's lookup (a DM core doesn't know:
// conversations.info first) waits for that lookup alone, not for the
// lookups of other socket events (pumpLater's queue: a pref_change's
// users.prefs). Queued behind them, a new DM waited for a users.prefs
// that a frecency save brings about once a minute.
func TestHeldDMDoesntWaitForPrefs(t *testing.T) {
	a, f := testApp(t)
	evs, stop := a.Subscribe()
	defer stop()
	gate := make(chan struct{})
	defer func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}()
	f.mu.Lock()
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"D9","is_im":true,"user":"U2"}}`
	f.reply["users.prefs.get"] = `{"ok":true,"prefs":{}}`
	f.hooks["users.prefs.get"] = func(url.Values) { <-gate }
	f.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.handle(rtm("pref_change", map[string]any{"name": "display_real_names", "value": true}))
		a.handle(rtm("message", map[string]any{"channel": "D9", "ts": "1762516900.000100", "user": "U2", "text": "hi"}))
	}()
	var got []Event
	if !awaitMessage(evs, "D9", "1762516900.000100", 5*time.Second, &got) {
		t.Error("the new DM's message waited for the pref_change's users.prefs")
	}
	close(gate)
	<-done
	a.laterDone(t)
}
