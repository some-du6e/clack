package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Round 5's core tests: a message_changed of a message older than the
// window stays out of it, an answer without a message drops its fetched
// copy, a read's revision goes out after the pins and subscriptions it
// syncs, the reactions a read keeps, and Activity's badges' zero without
// the inbox.

// rangeWindow is C1's window: the newest ten messages, with older history
// beyond them (HasMore).
func rangeWindow(a *App) []slack.Message {
	var page []slack.Message
	for i := 0; i < 10; i++ {
		page = append(page, slack.Message{Type: "message", TS: fmt.Sprintf("1762795%03d.000100", 10+i), User: "U1", Text: fmt.Sprintf("synthetic %d", i)})
	}
	a.S.SetLatest("C1", page, true)
	return page
}

func timelineTSs(a *App, ch string) []string {
	w := a.S.Timeline(ch)
	if w == nil {
		return nil
	}
	out := make([]string, len(w.Msgs))
	for i := range w.Msgs {
		out[i] = w.Msgs[i].TS
	}
	return out
}

// A message_changed of a message older than the window core holds (a vote
// on a poll 80 messages back, a bot editing an old status, a root's new
// reply count after one of its replies is deleted from Threads) stays out
// of the window: taken in above its oldest message, it reached the open
// list at its next load, and load-older read from it, skipping the
// history between. Slack's invH keeps such a ts as a slice of its own,
// drawn only once a page covering it loads. The copies fetched by id
// (Activity's, Threads') take the change all the same. A window holding
// the whole history (no HasMore) takes it where it belongs.
func TestChangedOldMessageStaysOutOfWindow(t *testing.T) {
	a, _ := testApp(t)
	const ch, poll, root, reply = "C1", "1762790010.000100", "1762790020.000100", "1762790030.000100"
	page := rangeWindow(a)
	before := fmt.Sprint(timelineTSs(a, ch))
	a.S.PutFetched(ch, []slack.Message{{Type: "message", TS: poll, User: "U2", Text: "votes: 1"}}, a.S.LiveSeq()) // Activity's copy
	a.handle(rtm("message", map[string]any{"subtype": "message_changed", "hidden": true, "channel": ch, "ts": "1762795100.000100",
		"message":          map[string]any{"type": "message", "ts": poll, "user": "U2", "text": "votes: 2"},
		"previous_message": map[string]any{"type": "message", "ts": poll, "user": "U2", "text": "votes: 1"}}))
	if got := fmt.Sprint(timelineTSs(a, ch)); got != before {
		t.Fatalf("the window after an older message's message_changed: %s, want %s", got, before)
	}
	if m, ok := a.S.FindHeld(ch, poll); !ok || m.Text != "votes: 2" {
		t.Errorf("the fetched copy after the change: %q (%v)", m.Text, ok)
	}
	// a reply deleted from Threads: the message_deleted, then the root's
	// message_changed with its new reply count (pushDeleteReply's shape)
	a.S.PutFetched(ch, []slack.Message{
		{Type: "message", TS: root, ThreadTS: root, User: "U1", Text: "a synthetic root", ReplyCount: 1, LatestReply: reply},
		{Type: "message", TS: reply, ThreadTS: root, User: "USELF", Text: "a synthetic reply"},
	}, a.S.LiveSeq()) // Threads' copies
	a.handle(rtm("message", map[string]any{"subtype": "message_deleted", "hidden": true, "channel": ch, "deleted_ts": reply,
		"previous_message": map[string]any{"type": "message", "ts": reply, "thread_ts": root, "user": "USELF", "text": "a synthetic reply"}}))
	a.handle(rtm("message", map[string]any{"subtype": "message_changed", "hidden": true, "channel": ch, "ts": "1762795101.000100",
		"message": map[string]any{"type": "message", "ts": root, "user": "U1", "text": "a synthetic root"}}))
	if got := fmt.Sprint(timelineTSs(a, ch)); got != before {
		t.Fatalf("the window after an older root's message_changed: %s, want %s", got, before)
	}
	// one inside the window's range that it lacks goes in
	in := "1762795012.500100"
	a.handle(rtm("message", map[string]any{"subtype": "message_changed", "hidden": true, "channel": ch, "ts": "1762795102.000100",
		"message": map[string]any{"type": "message", "ts": in, "user": "U1", "text": "a synthetic message the window missed"}}))
	if tss := timelineTSs(a, ch); len(tss) != len(page)+1 {
		t.Errorf("a message_changed inside the window's range stayed out: %v", tss)
	}
	// with the whole history in the window, an older message has its place
	a.S.SetLatest(ch, page, false)
	a.handle(rtm("message", map[string]any{"subtype": "message_changed", "hidden": true, "channel": ch, "ts": "1762795103.000100",
		"message": map[string]any{"type": "message", "ts": poll, "user": "U2", "text": "votes: 3"}}))
	if tss := timelineTSs(a, ch); len(tss) == 0 || tss[0] != poll {
		t.Errorf("a window holding the whole history left out an older message's change: %v", tss)
	}
}

// A messages.list answer that leaves out a message it was asked for (the
// channel answered: the message is gone, Slack's isNonExistent) takes the
// store's fetched copy of it away too, in the same write: a copy from an
// earlier read drew the deleted message again once the Activity page fell
// behind the change table's floor, or went out with a thread_subscribed.
func TestHydrateDropsMissingFetched(t *testing.T) {
	a, f := testApp(t)
	const ch, gone, kept = "C1", "1762795200.000100", "1762795210.000100"
	a.S.PutFetched(ch, []slack.Message{{Type: "message", TS: gone, User: "U1", Text: "a synthetic message, deleted while the socket was down"}}, a.S.LiveSeq())
	b, _ := json.Marshal(map[string]any{"ok": true, "messages": map[string]any{ch: []any{
		map[string]any{"type": "message", "ts": kept, "user": "U1", "text": "a synthetic message still there"}}}})
	f.reply["messages.list"] = string(b)
	got, answered := a.hydrateOK(context.Background(), map[string][]string{ch: {gone, kept}})
	if !answered[ch] {
		t.Fatal("the channel didn't answer")
	}
	if _, ok := got[ch+"/"+gone]; ok {
		t.Fatal("the hydrate returned the deleted message")
	}
	if m, ok := a.S.FindHeld(ch, gone); ok {
		t.Errorf("the store still holds the deleted message: %q", m.Text)
	}
	if _, ok := a.HeldMessage(ch, gone); ok {
		t.Error("HeldMessage still has the deleted message")
	}
	if _, ok := a.S.FindHeld(ch, kept); !ok {
		t.Error("the store let the message the answer had go")
	}
}

// A pin that lands while a history read is out goes out with the read's
// revision: the pins are synced before core announces what the read
// brought, so the announced copy is pinned, as the store's is after.
func TestReadRevisionKeepsPinDuringRead(t *testing.T) {
	a, f := testApp(t)
	const ch, ts = "C1", "1762795300.000100"
	a.S.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "U1", Text: "votes: 1"}}, false)
	f.reply["conversations.history"] = pageJSON(liveVote(ts, 2))
	f.hooks["conversations.history"] = func(url.Values) {
		a.handle(rtm("pin_added", map[string]any{"user": "U2", "channel_id": ch, "event_ts": "1762795301.000100",
			"item": map[string]any{"type": "message", "channel": ch, "created_by": "U2", "created": 1762795301,
				"message": map[string]any{"ts": ts, "user": "U1", "text": "votes: 1"}}}))
	}
	if _, err := a.Refresh(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	c, ok := a.MessageChange(ch, ts)
	if !ok || c.Msg == nil || c.Msg.Text != "votes: 2" {
		t.Fatalf("the read's revision wasn't announced: %+v (%v)", c, ok)
	}
	if !c.Msg.Pinned {
		t.Error("the announced revision isn't pinned; the pin landed while the read was out")
	}
}

// A reaction that lands while the newest page is out stays on the page's
// revision: the window keeps the page's copy (votes: 2, newer than the
// window's) with the reaction replayed on it. A live record of the
// reaction (msg-latency's NoteLive after a reaction, merged) made the
// read keep the held copy instead, the older text with it.
func TestWindowReadKeepsPageRevisionWithReaction(t *testing.T) {
	a, f := testApp(t)
	const ch, ts = "C1", "1762795400.000100"
	a.S.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "U1", Text: "votes: 1"}}, false)
	f.reply["conversations.history"] = pageJSON(liveVote(ts, 2))
	f.hooks["conversations.history"] = func(url.Values) {
		a.handle(rtm("reaction_added", map[string]any{"user": "U2", "reaction": "tada", "item": map[string]any{"type": "message", "channel": ch, "ts": ts}}))
	}
	if _, err := a.Refresh(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	w := a.S.Timeline(ch)
	if w == nil || len(w.Msgs) != 1 {
		t.Fatalf("the window: %+v", w)
	}
	if m := w.Msgs[0]; m.Text != "votes: 2" || len(m.Reactions) != 1 || m.Reactions[0].Name != "tada" {
		t.Errorf("the window after the read: %q %+v, want votes: 2 with the tada", m.Text, m.Reactions)
	}
}

// Without the inbox, Slack's badge_counts_updated marks the All list's
// unread rows read when every count and alert it carries is 0 (VxbD
// handleBadgeCountsUpdatedRTMEvent's other branch): the event says so,
// and only then.
func TestClassicBadgesZero(t *testing.T) {
	a, _ := testApp(t)
	a.SetExperimentGroup("activity_inbox", "off")
	evs, stop := a.Subscribe()
	defer stop()
	send := func(counts, alerts map[string]int) {
		raw, _ := json.Marshal(map[string]any{"type": "badge_counts_updated", "counts": counts, "alerts": alerts})
		a.handle(slack.Event{Type: "badge_counts_updated", Raw: raw})
	}
	zero := func() (bool, bool) {
		for {
			select {
			case ev := <-evs:
				if ev.Type == "activity_badges" {
					return ev.Name == ActivityBadgesZero, true
				}
			case <-time.After(300 * time.Millisecond):
				return false, false
			}
		}
	}
	send(map[string]int{"channels": 0, "dms": 0}, map[string]int{"external_channel_invite": 0})
	if z, ok := zero(); !ok || !z {
		t.Errorf("every count and alert at 0: zero %v (an event: %v), want the zero", z, ok)
	}
	send(map[string]int{"channels": 2, "dms": 0}, map[string]int{"external_channel_invite": 0})
	if z, _ := zero(); z {
		t.Error("a count above 0 marked the rows read")
	}
	send(nil, nil)
	if z, _ := zero(); z {
		t.Error("an event with no counts nor alerts marked the rows read (Slack's needs at least one)")
	}
}
